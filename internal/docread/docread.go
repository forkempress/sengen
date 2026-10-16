// Package docread reads a scanned history (PDF or photo) with Claude and
// turns it into the CSV that internal/importer already parses, so a document
// goes through exactly the same validation, preview and confirm step as an
// uploaded CSV file. Nothing here writes to the database.
package docread

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// Limits on what is sent to the model. A firm has at most ~10 drivers and
// ~100 stays; anything bigger is handled by hand ("pošaljite nam na mejl").
const (
	MaxFileBytes = 10 << 20
	MaxPages     = 10
	MaxDrivers   = 10
	MaxStays     = 150
)

// Timeout bounds one model call. It stays under the ~100 s a Cloudflare
// tunnel waits for a response.
const Timeout = 90 * time.Second

const defaultModel = "claude-opus-5-5"

// Media types the model accepts.
var mediaTypes = map[string]bool{
	"application/pdf": true,
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
}

// Supported reports whether mediaType can be read.
func Supported(mediaType string) bool { return mediaTypes[mediaType] }

// Stay is one stay as read from the document. Dates are dd.mm.yyyy, Exit is
// empty while the driver is still in the Schengen area.
type Stay struct {
	Driver    string `json:"driver"`
	Entry     string `json:"entry"`
	Exit      string `json:"exit"`
	Type      string `json:"type"`
	Note      string `json:"note"`
	Uncertain bool   `json:"uncertain"`
}

// Result is what the model read. Reason is set when it found no stays.
// Usage is filled whenever the model answered, even if Read then fails.
type Result struct {
	Stays  []Stay `json:"stays"`
	Reason string `json:"reason"`
	Usage  Usage  `json:"-"`
}

// Usage is what one call cost.
type Usage struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
	CostMicroUSD int64
}

// prices are US cents per million input and output tokens (Haiku costs a
// fraction of a dollar). A refused request may be answered by a fallback
// model, so the answering model is priced, not the one asked for. An unknown
// model is priced as the default. Haiku's price doubles past 100K input
// tokens, which a 10-page document does not reach.
var prices = map[string][2]int64{
	"claude-opus-5-5":   {400, 2000},
	"claude-opus-5":     {500, 2500},
	"claude-opus-4-8":   {500, 2500},
	"claude-sonnet-5-5": {200, 1000},
	"claude-haiku-5-5":  {10, 50},
	"claude-fable-5-1":  {1000, 5000},
}

// cost prices a call in millionths of a dollar: tokens × $/Mtok is exactly
// that unit, and the prices are in cents.
func cost(model string, in, out int64) int64 {
	p, ok := prices[model]
	if !ok {
		p = prices[defaultModel]
	}
	return (in*p[0] + out*p[1]) / 100
}

// ErrTooLarge means the file or what was read from it is over the limits.
var ErrTooLarge = errors.New("dokument je prevelik")

// Reader reads documents. The web server holds one; nil turns the feature off.
type Reader interface {
	Read(ctx context.Context, data []byte, mediaType string, driverNames []string) (Result, error)
}

// Claude reads documents through the Claude API.
type Claude struct {
	client anthropic.Client
	model  string
}

func NewClaude(apiKey string) *Claude { return NewClaudeModel(apiKey, defaultModel) }

// NewClaudeModel reads with the given model; the document eval uses it to
// compare models on the same scans.
func NewClaudeModel(apiKey, model string) *Claude {
	return &Claude{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: model}
}

// noFallback lists models without a server-side refusal fallback; sending
// the fallbacks parameter to them is rejected.
var noFallback = map[string]bool{"claude-haiku-5-5": true}

// pdfPageRe counts page objects in an uncompressed page tree. PDFs that keep
// their objects in compressed streams count as zero and are left to the
// size limit and the API's own page limit.
var pdfPageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

// CheckFile applies the size and page limits before anything is sent.
func CheckFile(data []byte, mediaType string) error {
	if len(data) > MaxFileBytes {
		return ErrTooLarge
	}
	if mediaType == "application/pdf" && len(pdfPageRe.FindAllIndex(data, -1)) > MaxPages {
		return ErrTooLarge
	}
	return nil
}

const systemPrompt = `Čitaš skeniranu evidenciju boravaka vozača u Šengen zoni za srpsku prevozničku firmu. Dokument može biti tabela, štampani izveštaj, rukom pisana sveska ili nešto drugo.

Izvuci svaki boravak: ime vozača, datum ulaska, datum izlaska, tip (posao ili privatno) i kratku napomenu ako je ima.

Pravila:
- Datume piši kao dd.mm.gggg. Dokument je iz Srbije, pa je 03.04. treći april, ne četvrti mart.
- Ako izlaz nedostaje jer je vozač još u Šengenu, ostavi exit prazan.
- Ako tip nije naveden, stavi "posao".
- Ime vozača napiši tačno kao u spisku postojećih vozača kad je jasno da je isti čovek (npr. bez kvačica, obrnut redosled imena i prezimena, samo prezime). Ako ga nema u spisku, prepiši ime kako piše u dokumentu.
- Ako neki datum ili ime nisi siguran da si dobro pročitao, upiši najbolju procenu i stavi uncertain na true.
- Ne izmišljaj boravke kojih nema u dokumentu.
- Ako dokument nije evidencija boravaka ili je nečitak, vrati praznu listu stays i u reason kratko na srpskom objasni zašto.`

var resultSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"stays": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"driver":    map[string]any{"type": "string"},
					"entry":     map[string]any{"type": "string", "description": "dd.mm.gggg"},
					"exit":      map[string]any{"type": "string", "description": "dd.mm.gggg ili prazno"},
					"type":      map[string]any{"type": "string", "enum": []string{"posao", "privatno"}},
					"note":      map[string]any{"type": "string"},
					"uncertain": map[string]any{"type": "boolean"},
				},
				"required":             []string{"driver", "entry", "exit", "type", "note", "uncertain"},
				"additionalProperties": false,
			},
		},
		"reason": map[string]any{"type": "string"},
	},
	"required":             []string{"stays", "reason"},
	"additionalProperties": false,
}

func (c *Claude) Read(ctx context.Context, data []byte, mediaType string, driverNames []string) (Result, error) {
	if !Supported(mediaType) {
		return Result{}, fmt.Errorf("nepodržan tip fajla %q", mediaType)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	var doc anthropic.BetaContentBlockParamUnion
	if mediaType == "application/pdf" {
		doc = anthropic.NewBetaDocumentBlock(anthropic.BetaBase64PDFSourceParam{Data: b64})
	} else {
		doc = anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
			Data: b64, MediaType: anthropic.BetaBase64ImageSourceMediaType(mediaType),
		})
	}
	names := "(nema unetih vozača)"
	if len(driverNames) > 0 {
		names = strings.Join(driverNames, "\n")
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	params := anthropic.BetaMessageNewParams{
		Model:     c.model,
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(
			doc,
			anthropic.NewBetaTextBlock("Postojeći vozači firme:\n"+names),
		)},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortMedium,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: resultSchema},
		},
	}
	if !noFallback[c.model] {
		// A refused request is answered by a fallback model in the same call.
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	resp, err := c.client.Beta.Messages.New(ctx, params, option.WithMaxRetries(1))
	if err != nil {
		return Result{}, err
	}
	usage := Usage{
		Model:        string(resp.Model),
		InputTokens:  resp.Usage.InputTokens + resp.Usage.CacheCreationInputTokens + resp.Usage.CacheReadInputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}
	usage.CostMicroUSD = cost(usage.Model, usage.InputTokens, usage.OutputTokens)
	switch resp.StopReason {
	case anthropic.BetaStopReasonEndTurn:
	case anthropic.BetaStopReasonMaxTokens:
		return Result{Usage: usage}, ErrTooLarge
	default:
		return Result{Usage: usage}, fmt.Errorf("model je stao: %s", resp.StopReason)
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	res := Result{Usage: usage}
	if err := json.Unmarshal([]byte(text.String()), &res); err != nil {
		return Result{Usage: usage}, fmt.Errorf("neispravan odgovor modela: %w", err)
	}
	return res, nil
}

// CheckResult applies the driver and stay limits to what was read.
func CheckResult(res Result) error {
	if len(res.Stays) > MaxStays || DriverCount(res.Stays) > MaxDrivers {
		return ErrTooLarge
	}
	return nil
}

// DriverCount counts distinct driver names, ignoring case and spaces.
func DriverCount(stays []Stay) int {
	drivers := map[string]bool{}
	for _, s := range stays {
		drivers[strings.ToLower(strings.TrimSpace(s.Driver))] = true
	}
	return len(drivers)
}

// CSV writes the stays in the importer's column order (vozač, ulaz, izlaz,
// tip, napomena) with a header row, so stay i ends up on CSV line i+2.
func CSV(stays []Stay) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"vozač", "ulaz", "izlaz", "tip", "napomena"})
	for _, s := range stays {
		w.Write([]string{s.Driver, s.Entry, s.Exit, s.Type, s.Note})
	}
	w.Flush()
	return buf.Bytes()
}
