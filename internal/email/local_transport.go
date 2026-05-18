package email

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LocalSpoolDir is where the disk-spool fallback drops emails when neither
// SMTP nor sendmail is available. Overridable for tests.
var LocalSpoolDir = "./tmp/mail"

var (
	sendmailLookupOnce sync.Once
	sendmailPath       string
)

// sendmailBinary returns the path to a local sendmail-compatible MTA, or "" if
// none is available. Cached after the first lookup.
func sendmailBinary() string {
	sendmailLookupOnce.Do(func() {
		// Try the conventional paths first (most distros symlink these).
		for _, p := range []string{"/usr/sbin/sendmail", "/usr/lib/sendmail"} {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				sendmailPath = p
				return
			}
		}
		// Fall back to PATH lookup.
		if p, err := exec.LookPath("sendmail"); err == nil {
			sendmailPath = p
		}
	})
	return sendmailPath
}

// sendViaSendmail pipes the assembled RFC-5322 message into the local
// sendmail binary using `-t -i` so the recipient list is read from the
// To/Cc/Bcc headers and bare dots are not interpreted as message-end.
func sendViaSendmail(bin string, message []byte) error {
	cmd := exec.Command(bin, "-t", "-i")
	cmd.Stdin = bytes.NewReader(message)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sendmail %s exited with error: %w (output: %s)", bin, err, string(out))
	}
	return nil
}

// spoolToDisk writes the assembled message to a timestamped .eml file under
// LocalSpoolDir. Used when no SMTP and no sendmail are available, so the
// admin can still see what would have been sent (and forward it manually).
func spoolToDisk(toEmail string, message []byte) (string, error) {
	if err := os.MkdirAll(LocalSpoolDir, 0o755); err != nil {
		return "", fmt.Errorf("create mail spool dir: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000")
	safe := sanitizeAddr(toEmail)
	path := filepath.Join(LocalSpoolDir, fmt.Sprintf("%s-%s.eml", stamp, safe))
	if err := os.WriteFile(path, message, 0o600); err != nil {
		return "", fmt.Errorf("write spool file: %w", err)
	}
	return path, nil
}

func sanitizeAddr(addr string) string {
	out := make([]byte, 0, len(addr))
	for i := 0; i < len(addr); i++ {
		c := addr[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '_':
			out = append(out, c)
		case c == '@':
			out = append(out, '_')
		default:
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

// deliverWithoutSMTP is the fallback path used by SendEmailActual when SMTP
// isn't configured. It tries sendmail first, then disk-spool. Returns nil on
// success and logs the chosen path so admins can trace where the message went.
func deliverWithoutSMTP(toEmail string, message []byte) error {
	if bin := sendmailBinary(); bin != "" {
		if err := sendViaSendmail(bin, message); err != nil {
			log.Warn().Err(err).Str("email", maskEmail(toEmail)).Str("sendmail", bin).
				Msg("sendmail delivery failed; falling back to disk spool")
		} else {
			log.Debug().Str("email", maskEmail(toEmail)).Str("sendmail", bin).Msg("📧 Email delivered via local sendmail")
			return nil
		}
	}

	path, err := spoolToDisk(toEmail, message)
	if err != nil {
		return fmt.Errorf("no SMTP, no sendmail, and disk spool failed: %w", err)
	}
	log.Warn().
		Str("email", maskEmail(toEmail)).
		Str("spool_path", path).
		Msg("📥 Email spooled to disk (no SMTP / sendmail configured)")
	return nil
}

