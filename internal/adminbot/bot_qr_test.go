package adminbot

import (
	"errors"
	"os"
	"testing"
)

func TestPairingQRBoundsCentersWithinAvailableSpace(t *testing.T) {
	bounds, err := pairingQRBounds(120, 60, 3)
	if err != nil {
		t.Fatalf("pairingQRBounds returned error: %v", err)
	}

	if bounds.Min.Y != 3 {
		t.Fatalf("expected qr to start on cursor row 3, got %d", bounds.Min.Y)
	}
	if bounds.Dx() != pairingQRMaxCells || bounds.Dy() != pairingQRMaxCells {
		t.Fatalf("expected max %dx%d qr bounds, got %dx%d", pairingQRMaxCells, pairingQRMaxCells, bounds.Dx(), bounds.Dy())
	}
	if bounds.Min.X != 42 {
		t.Fatalf("expected centered x offset 42, got %d", bounds.Min.X)
	}
}

func TestPairingQRBoundsShrinksToFitHeight(t *testing.T) {
	bounds, err := pairingQRBounds(80, 24, 8)
	if err != nil {
		t.Fatalf("pairingQRBounds returned error: %v", err)
	}

	if bounds.Dx() != 14 || bounds.Dy() != 14 {
		t.Fatalf("expected height-constrained 14x14 bounds, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	if bounds.Min.Y != 8 {
		t.Fatalf("expected qr to start on cursor row 8, got %d", bounds.Min.Y)
	}
}

func TestPairingQRBoundsErrorsWhenTerminalTooSmall(t *testing.T) {
	if _, err := pairingQRBounds(12, 12, 0); err == nil {
		t.Fatal("expected terminal-too-small error, got nil")
	}
}

func TestShouldDisableTerminalQR(t *testing.T) {
	if shouldDisableTerminalQR(nil) {
		t.Fatal("nil error should not disable terminal qr")
	}

	if shouldDisableTerminalQR(errors.New("plain qr render failure")) {
		t.Fatal("generic filesystem error should not disable terminal qr")
	}

	if !shouldDisableTerminalQR(testError("open terminal for qr rendering: no/failed tty provision;: nil tty provider\nopen /dev/tty: no such device or address")) {
		t.Fatal("expected no-tty termimg error to disable terminal qr")
	}
}

func TestEnsurePairingQRFileReusesStablePath(t *testing.T) {
	bot := &Bot{}

	firstPath, created, err := bot.ensurePairingQRFile()
	if err != nil {
		t.Fatalf("ensurePairingQRFile returned error: %v", err)
	}
	if !created {
		t.Fatal("expected first pairing qr path to be newly created")
	}
	defer os.Remove(firstPath)

	secondPath, createdAgain, err := bot.ensurePairingQRFile()
	if err != nil {
		t.Fatalf("ensurePairingQRFile second call returned error: %v", err)
	}
	if createdAgain {
		t.Fatal("expected second pairing qr path to be reused")
	}
	if firstPath != secondPath {
		t.Fatalf("expected stable pairing qr path, got %q then %q", firstPath, secondPath)
	}
}

func TestEnsurePairingQRFilePrefersStoreDirectory(t *testing.T) {
	storeDir := t.TempDir()
	bot := &Bot{
		cfg: &Config{
			WhatsApp: WhatsAppConfig{
				StorePath: storeDir + "/whatsapp.db",
			},
		},
	}

	path, created, err := bot.ensurePairingQRFile()
	if err != nil {
		t.Fatalf("ensurePairingQRFile returned error: %v", err)
	}
	if !created {
		t.Fatal("expected qr file in store directory to be created on first call")
	}

	want := storeDir + "/pairing-qr.png"
	if path != want {
		t.Fatalf("expected preferred pairing qr path %q, got %q", want, path)
	}
}

type testError string

func (e testError) Error() string { return string(e) }
