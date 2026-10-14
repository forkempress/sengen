package web

import (
	"encoding/csv"
	"net/http"
	"time"

	"shengen/internal/clock"
	"shengen/internal/tenant"
)

// sampleRow is one row of the example CSV, in days before today so the
// sample never contains a future date the import would refuse.
type sampleRow struct {
	driver            int // index into the names used
	entryAgo, exitAgo int // exitAgo < 0: still in Schengen
	typ, note         string
}

var sampleRows = []sampleRow{
	{0, 60, 51, "posao", ""},
	{0, 9, -1, "posao", "još je u Šengenu"},
	{1, 40, 33, "posao", ""},
	{1, 20, 16, "privatno", "godišnji odmor"},
}

// sampleCSV builds the example file shown and offered on the import page.
// It uses the firm's own driver names when it has them, so the file can be
// edited and imported as is.
func sampleCSV(names []string, today time.Time) [][]string {
	fallback := []string{"Marko Jovanović", "Nikola Petrović"}
	for i := len(names); i < len(fallback); i++ {
		names = append(names, fallback[i])
	}
	out := [][]string{{"vozač", "ulaz", "izlaz", "tip", "napomena"}}
	for _, r := range sampleRows {
		exit := ""
		if r.exitAgo >= 0 {
			exit = today.AddDate(0, 0, -r.exitAgo).Format("02.01.2006")
		}
		out = append(out, []string{names[r.driver], today.AddDate(0, 0, -r.entryAgo).Format("02.01.2006"), exit, r.typ, r.note})
	}
	return out
}

func (s *Server) sampleNames(r *http.Request) []string {
	t, _ := tenant.FromContext(r.Context())
	drivers, err := s.Store.ListDrivers(r.Context(), t.ID, true)
	if err != nil {
		return nil
	}
	var names []string
	for _, d := range drivers {
		if len(names) == 2 {
			break
		}
		names = append(names, d.Name)
	}
	return names
}

// handleImportSample serves the example as a file. The UTF-8 byte order
// mark lets Excel show č, ć, š, ž and đ correctly.
func (s *Server) handleImportSample(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="dani90-primer.csv"`)
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	cw.WriteAll(sampleCSV(s.sampleNames(r), clock.Today()))
}
