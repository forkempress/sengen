// Command doceval compares models on reading scanned stay histories. Every
// scan in -dir (PDF, JPG, PNG, WEBP) needs the correct stays next to it in a
// CSV of the same name, in the import format (vozač, ulaz, izlaz, tip,
// napomena). The model gets the drivers of that CSV as the firm's existing
// drivers, unless <scan>.vozaci.txt lists them (one per line).
//
//	ANTHROPIC_API_KEY=... go run ./cmd/doceval
//
// With -db it instead takes the scans users uploaded in the app (kept 30
// days) from a copy of the database. The correct stays are the ones now in
// the app for the drivers each import wrote to, over the period it covered,
// so fixes made on the driver's page count. A mistake nobody fixed counts as
// correct, which favors the model that read the scan in the app; the report
// names it per scan.
//
// Each call costs money. Rows land in <out>/results.jsonl as they finish;
// running again with the same -out skips what is already there. The summary
// is printed and written with every mistake to <out>/izvestaj.md.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"shengen/internal/db"
	"shengen/internal/docread"
	"shengen/internal/store"
)

var mediaTypes = map[string]string{
	".pdf": "application/pdf", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".png": "image/png", ".webp": "image/webp",
}

type scan struct {
	name      string
	data      []byte
	mediaType string
	want      []docread.Stay
	drivers   []string
	// single: every row goes on this driver, as when read on his page.
	single string
	// note says where the scan came from, for the report.
	note string
}

// Row is one graded call.
type Row struct {
	Model       string         `json:"model"`
	Scan        string         `json:"scan"`
	Rep         int            `json:"rep"`
	ServedModel string         `json:"served_model"`
	Seconds     float64        `json:"seconds"`
	InputTok    int64          `json:"input_tokens"`
	OutputTok   int64          `json:"output_tokens"`
	CostMicro   int64          `json:"cost_micro_usd"`
	Reason      string         `json:"reason,omitempty"`
	Error       string         `json:"error,omitempty"`
	Stays       []docread.Stay `json:"stays"`
	Grade       Grade          `json:"grade"`
}

func (r Row) key() string { return fmt.Sprintf("%s|%s|%d", r.Model, r.Scan, r.Rep) }

func main() {
	dir := flag.String("dir", "eval/dokumenti", "folder sa skenovima i tačnim rezultatima")
	dbPath := flag.String("db", "", "kopija baze: testira skenove koje su korisnici učitali u aplikaciji (umesto -dir)")
	out := flag.String("out", "eval/rezultati/"+time.Now().Format("2006-01-02-1504"), "folder za rezultate (isti folder nastavlja prekinut test)")
	modelsArg := flag.String("models", "opus,sonnet,haiku", "modeli, odvojeni zarezom (opus, sonnet, haiku ili pun ID)")
	reps := flag.Int("reps", 2, "koliko puta se svaki skan čita istim modelom")
	parallel := flag.Int("parallel", 4, "koliko poziva ide istovremeno")
	flag.Parse()

	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY nije postavljen")
	}
	var models []string
	for _, m := range strings.Split(*modelsArg, ",") {
		if strings.TrimSpace(m) == "" {
			continue
		}
		id, ok := docread.ResolveModel(m)
		if !ok {
			log.Fatalf("nepoznat model %q", m)
		}
		models = append(models, id)
	}
	var scans []scan
	var err error
	if *dbPath != "" {
		scans, err = loadFromDB(*dbPath)
	} else {
		scans, err = loadScans(*dir)
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	resultsPath := filepath.Join(*out, "results.jsonl")
	done, err := readRows(resultsPath)
	if err != nil {
		log.Fatal(err)
	}
	have := map[string]bool{}
	for _, r := range done {
		have[r.key()] = true
	}

	type job struct {
		model string
		scan  scan
		rep   int
	}
	var jobs []job
	for _, m := range models {
		for _, s := range scans {
			for rep := range *reps {
				if !have[Row{Model: m, Scan: s.name, Rep: rep}.key()] {
					jobs = append(jobs, job{m, s, rep})
				}
			}
		}
	}
	fmt.Printf("%d skenova, %d modela, %d ponavljanja: %d poziva (%d već urađeno)\n",
		len(scans), len(models), *reps, len(jobs), len(done))

	results, err := os.OpenFile(resultsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	defer results.Close()
	errorsFile, err := os.OpenFile(filepath.Join(*out, "errors.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	defer errorsFile.Close()

	reader := docread.NewClaude(apiKey, "")
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		sem  = make(chan struct{}, *parallel)
		left = len(jobs)
	)
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			row := run(reader, j.model, j.scan, j.rep)
			line, _ := json.Marshal(row)
			mu.Lock()
			defer mu.Unlock()
			left--
			// A failed call goes to errors.jsonl, so the next run retries it.
			if row.Error != "" {
				errorsFile.Write(append(line, '\n'))
				fmt.Printf("  [%d preostalo] %s %s #%d: GREŠKA %s\n", left, short(j.model), j.scan.name, j.rep+1, row.Error)
				return
			}
			results.Write(append(line, '\n'))
			done = append(done, row)
			fmt.Printf("  [%d preostalo] %s %s #%d: %d/%d tačno, $%.4f, %.0fs\n", left, short(j.model), j.scan.name, j.rep+1,
				row.Grade.Correct, row.Grade.Expected, float64(row.CostMicro)/1e6, row.Seconds)
		}()
	}
	wg.Wait()

	notes := map[string]string{}
	for _, s := range scans {
		notes[s.name] = s.note
	}
	report := summarize(done, models, notes)
	fmt.Print("\n" + report.summary)
	if err := os.WriteFile(filepath.Join(*out, "izvestaj.md"), []byte(report.full), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nDetalji: %s\n", filepath.Join(*out, "izvestaj.md"))
}

func run(r *docread.Claude, model string, s scan, rep int) Row {
	row := Row{Model: model, Scan: s.name, Rep: rep}
	start := time.Now()
	res, err := r.Read(context.Background(), s.data, s.mediaType, s.drivers, model)
	row.Seconds = time.Since(start).Seconds()
	row.ServedModel, row.InputTok, row.OutputTok, row.CostMicro = res.Usage.Model, res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.CostMicroUSD
	if err != nil {
		row.Error = err.Error()
		return row
	}
	if s.single != "" {
		for i := range res.Stays {
			res.Stays[i].Driver = s.single
		}
	}
	row.Stays, row.Reason = res.Stays, res.Reason
	row.Grade = grade(s.want, res.Stays)
	return row
}

func loadScans(dir string) ([]scan, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var scans []scan
	var problems []string
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		mt, ok := mediaTypes[ext]
		if e.IsDir() || !ok {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if err := docread.CheckFile(data, mt); err != nil {
			problems = append(problems, e.Name()+": "+err.Error())
			continue
		}
		f, err := os.Open(filepath.Join(dir, base+".csv"))
		if err != nil {
			problems = append(problems, e.Name()+": nema "+base+".csv sa tačnim rezultatima")
			continue
		}
		want, err := readExpected(f)
		f.Close()
		if err != nil {
			problems = append(problems, base+".csv: "+err.Error())
			continue
		}
		drivers := driverNames(want)
		if list, err := os.ReadFile(filepath.Join(dir, base+".vozaci.txt")); err == nil {
			drivers = nil
			for _, line := range strings.Split(string(list), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					drivers = append(drivers, line)
				}
			}
		}
		scans = append(scans, scan{name: e.Name(), data: data, mediaType: mt, want: want, drivers: drivers})
	}
	if len(problems) > 0 {
		return nil, errors.New("skenovi nisu spremni:\n  " + strings.Join(problems, "\n  "))
	}
	if len(scans) == 0 {
		return nil, fmt.Errorf("u %s nema skenova (pdf, jpg, png, webp)", dir)
	}
	return scans, nil
}

func readRows(path string) ([]Row, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []Row
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var r Row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

func short(model string) string { return docread.ShortName(model) }

// loadFromDB takes the kept scans of confirmed imports from a copy of the
// database.
func loadFromDB(path string) ([]scan, error) {
	// db.Open would create a missing file and run migrations on it.
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	conn, err := db.Open(path)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	docs, err := store.New(conn).ListEvalDocuments(context.Background())
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("u bazi nema sačuvanih skenova potvrđenih uvoza")
	}
	ext := map[string]string{"application/pdf": ".pdf", "image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}
	scans := make([]scan, 0, len(docs))
	for _, d := range docs {
		s := scan{
			name:      fmt.Sprintf("uvoz-%d%s", d.ImportID, ext[d.MediaType]),
			data:      d.Data,
			mediaType: d.MediaType,
			drivers:   d.DriverNames,
			note:      fmt.Sprintf("%s, %s; u aplikaciji pročitao %s", d.TenantName, d.CreatedAt.Format("02.01.2006"), short(d.Model)),
		}
		if d.SingleDriverID != nil {
			s.single = d.Drivers[*d.SingleDriverID]
		}
		for _, st := range d.Stays {
			stay := docread.Stay{Driver: d.Drivers[st.DriverID], Entry: st.Entry.Format("02.01.2006"), Type: "posao"}
			if st.Exit != nil {
				stay.Exit = st.Exit.Format("02.01.2006")
			}
			if st.Type == "private" {
				stay.Type = "privatno"
			}
			s.want = append(s.want, stay)
		}
		scans = append(scans, s)
	}
	return scans, nil
}

type report struct{ summary, full string }

func summarize(rows []Row, models []string, notes map[string]string) report {
	var b strings.Builder
	fmt.Fprintln(&b, "| model | tačno | pogrešno pročitano | izmišljeno | propušteno | skenova bez greške | greške označene nesigurno | lažno nesigurno | cena po skenu | vreme po skenu |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|---|---|---|---|")
	var fallbacks []string
	for _, m := range models {
		var n, perfect int
		var t Grade
		var costMicro int64
		var seconds float64
		for _, r := range rows {
			if r.Model != m {
				continue
			}
			n++
			t.Expected += r.Grade.Expected
			t.Correct += r.Grade.Correct
			t.Misread += r.Grade.Misread
			t.Extra += r.Grade.Extra
			t.Missing += r.Grade.Missing
			t.FlaggedErrors += r.Grade.FlaggedErrors
			t.FlaggedCorrect += r.Grade.FlaggedCorrect
			if r.Grade.Perfect {
				perfect++
			}
			costMicro += r.CostMicro
			seconds += r.Seconds
			if r.ServedModel != "" && r.ServedModel != m {
				fallbacks = append(fallbacks, fmt.Sprintf("%s #%d: tražen %s, odgovorio %s", r.Scan, r.Rep+1, m, r.ServedModel))
			}
		}
		if n == 0 {
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %s | %s | %d | $%.4f | %.0f s |\n", short(m),
			pct(t.Correct, t.Expected), t.Misread, t.Extra, t.Missing, pct(perfect, n),
			ratio(t.FlaggedErrors, t.Misread+t.Extra), t.FlaggedCorrect,
			float64(costMicro)/1e6/float64(n), seconds/float64(n))
	}
	if len(fallbacks) > 0 {
		fmt.Fprintf(&b, "\nOdgovorio drugi model (odbijanje pa fallback), ti redovi ne mere traženi model:\n- %s\n", strings.Join(fallbacks, "\n- "))
	}
	summary := b.String()

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Scan != rows[j].Scan {
			return rows[i].Scan < rows[j].Scan
		}
		if rows[i].Model != rows[j].Model {
			return rows[i].Model < rows[j].Model
		}
		return rows[i].Rep < rows[j].Rep
	})
	var d strings.Builder
	fmt.Fprintf(&d, "# Poređenje modela za čitanje skenova\n\n%s\n## Greške po skenu\n", summary)
	scanName := ""
	for _, r := range rows {
		if r.Scan != scanName {
			scanName = r.Scan
			fmt.Fprintf(&d, "\n### %s (%d boravaka)\n\n", r.Scan, r.Grade.Expected)
			if n := notes[r.Scan]; n != "" {
				fmt.Fprintf(&d, "%s\n\n", n)
			}
		}
		if r.Grade.Perfect {
			fmt.Fprintf(&d, "- **%s #%d**: bez greške\n", short(r.Model), r.Rep+1)
			continue
		}
		fmt.Fprintf(&d, "- **%s #%d**: %d/%d tačno\n", short(r.Model), r.Rep+1, r.Grade.Correct, r.Grade.Expected)
		if r.Reason != "" {
			fmt.Fprintf(&d, "  - model kaže: %s\n", r.Reason)
		}
		for _, mk := range r.Grade.Mistakes {
			switch {
			case mk.Want != nil && mk.Got != nil:
				fmt.Fprintf(&d, "  - pogrešno: %s → pročitao %s\n", stayString(mk.Want), stayString(mk.Got))
			case mk.Got != nil:
				fmt.Fprintf(&d, "  - izmislio: %s\n", stayString(mk.Got))
			default:
				fmt.Fprintf(&d, "  - propustio: %s\n", stayString(mk.Want))
			}
		}
	}
	return report{summary: summary, full: d.String()}
}

func pct(a, b int) string {
	if b == 0 {
		return "–"
	}
	return fmt.Sprintf("%.0f%% (%d/%d)", 100*float64(a)/float64(b), a, b)
}

func ratio(a, b int) string {
	if b == 0 {
		return "–"
	}
	return fmt.Sprintf("%d/%d", a, b)
}
