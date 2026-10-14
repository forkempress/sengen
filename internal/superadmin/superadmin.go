// Package superadmin gives the service operators (Telegram user ids in
// ADMIN_TELEGRAM_IDS) read-only commands and push notifications about the
// whole platform, through the same bot the firms use.
//
// Rules every command follows:
//   - the caller's Telegram user id is checked on every message, in a private
//     chat only (the bot enforces the private chat before calling in);
//   - every attempt, allowed or refused, is written to superadmin_audit_log,
//     and an allowed command whose audit row cannot be written is not run;
//   - a refused caller gets no reply from here: the message goes on to the
//     normal handling, so the commands look like any unknown command;
//   - output holds counts and firm-level fields only — never a driver's name,
//     stay dates or locations.
package superadmin

import (
	"context"
	"log"
	"strings"
	"unicode/utf8"

	"shengen/internal/store"
	"shengen/internal/telegram"
)

type Service struct {
	Store *store.Store

	admins   map[int64]bool
	adminIDs []int64

	// BackupHeartbeatFile is touched by the backup job after each
	// successful run; "" means no backup is set up.
	BackupHeartbeatFile string
}

func New(st *store.Store, adminIDs []int64, backupHeartbeatFile string) *Service {
	s := &Service{Store: st, admins: make(map[int64]bool, len(adminIDs)), BackupHeartbeatFile: backupHeartbeatFile}
	for _, id := range adminIDs {
		if !s.admins[id] {
			s.admins[id] = true
			s.adminIDs = append(s.adminIDs, id)
		}
	}
	return s
}

// IsAdmin reports whether a Telegram user id belongs to an operator.
func (s *Service) IsAdmin(telegramUserID int64) bool {
	return telegramUserID != 0 && s.admins[telegramUserID]
}

// Commands lists every superadmin command. A message starting with any of
// them is audited, whoever sends it.
var Commands = []string{"/admin", "/stats", "/mrr", "/firme", "/firma", "/trial", "/neaktivne", "/zdravlje"}

func isCommand(cmd string) bool {
	for _, c := range Commands {
		if c == cmd {
			return true
		}
	}
	return false
}

// maxAuditArgs caps what is stored from a command's arguments.
const maxAuditArgs = 64

// parseCommand splits "/firma@dani90bot 12" into "/firma" and "12".
func parseCommand(text string) (cmd, args string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", ""
	}
	cmd = strings.ToLower(fields[0])
	if i := strings.IndexByte(cmd, '@'); i > 0 {
		cmd = cmd[:i]
	}
	args = strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	return cmd, args
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

const auditFailedMsg = "Komanda nije izvršena: zapis u audit log nije uspeo. Pokušajte ponovo."

// HandleMessage implements telegram.SuperAdminHandler. The bot calls it only
// for a private chat written in by fromID, so chatID == fromID.
func (s *Service) HandleMessage(ctx context.Context, client *telegram.Client, chatID, fromID int64, text string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	cmd, args := parseCommand(text)
	if !isCommand(cmd) {
		return false
	}
	allowed := s.IsAdmin(fromID)
	auditErr := s.Store.RecordSuperadminAudit(ctx, fromID, cmd, truncate(args, maxAuditArgs), allowed)
	if auditErr != nil {
		log.Printf("superadmin: audit %s from %d: %v", cmd, fromID, auditErr)
	}
	if !allowed {
		return false
	}
	if auditErr != nil {
		client.SendMessage(ctx, chatID, auditFailedMsg, nil)
		return true
	}

	reply, err := s.Run(ctx, client, cmd, args)
	if err != nil {
		log.Printf("superadmin: %s: %v", cmd, err)
		reply = "Greška pri učitavanju, pokušajte ponovo."
	}
	if err := client.SendMessage(ctx, chatID, reply, nil); err != nil {
		log.Printf("superadmin: reply to %s: %v", cmd, err)
	}
	return true
}

// Run produces the reply to one command. It does no access check — callers
// must have done it (HandleMessage does). client may be nil; /zdravlje then
// skips the Telegram check.
func (s *Service) Run(ctx context.Context, client *telegram.Client, cmd, args string) (string, error) {
	switch cmd {
	case "/admin":
		return helpText, nil
	case "/stats":
		return s.cmdStats(ctx)
	case "/mrr":
		return s.cmdMRR(ctx)
	case "/firme":
		return s.cmdFirme(ctx)
	case "/firma":
		return s.cmdFirma(ctx, args)
	case "/trial":
		return s.cmdTrial(ctx)
	case "/neaktivne":
		return s.cmdNeaktivne(ctx)
	case "/zdravlje":
		return s.cmdZdravlje(ctx, client), nil
	}
	return helpText, nil
}

const helpText = `Superadmin komande (samo čitanje):
/stats — firme, korisnici, vozači, boravci (zbirno)
/mrr — mesečni prihod
/firme — spisak firmi
/firma <id> — detalji jedne firme
/trial — firme u trialu, po isteku
/neaktivne — firme bez aktivnosti 14+ dana
/zdravlje — stanje servera, bota i backupa`
