package syncurl

import "testing"

func TestValidateAcceptsADirectConnectionString(t *testing.T) {
	for _, raw := range []string{
		"mongodb://host:10260/?tls=true",
		"  mongodb://user:pass@host/db  ",
		"mongodb://localhost:27017",
	} {
		if problem := Validate(raw); problem != None {
			t.Errorf("expected %q to be accepted, got %q", raw, problem)
		}
	}
}

func TestValidateRefusesSRV(t *testing.T) {
	// The one that matters: it works on the machine it is typed on and never
	// connects on Windows, so accepting it would hand someone a sync that
	// silently does nothing.
	if problem := Validate("mongodb+srv://cluster.example.com/db"); problem != SRV {
		t.Errorf("expected srv, got %q", problem)
	}
}

func TestValidateRefusesSomethingElseEntirely(t *testing.T) {
	for _, raw := range []string{
		"https://example.com",
		"postgres://host/db",
		"host:10260",
	} {
		if problem := Validate(raw); problem != NotMongo {
			t.Errorf("expected %q to be refused as not-mongo, got %q", raw, problem)
		}
	}
}

func TestValidateTreatsBlankAsEmpty(t *testing.T) {
	// Distinguished from a wrong URL: nothing typed is not a mistake, and the
	// window should not scold someone who simply has not filled it in.
	for _, raw := range []string{"", "   ", "\t\n"} {
		if problem := Validate(raw); problem != Empty {
			t.Errorf("expected empty for %q, got %q", raw, problem)
		}
	}
}

func TestDatabaseFallsBack(t *testing.T) {
	// The name matters far less than the URL, and demanding both would turn an
	// optional feature into a form.
	if got := Database("  "); got != DefaultDatabase {
		t.Errorf("expected the default, got %q", got)
	}
	if got := Database(" mangas "); got != "mangas" {
		t.Errorf("expected the trimmed name, got %q", got)
	}
}
