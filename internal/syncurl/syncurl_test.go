package syncurl

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
)

// answers builds a LookupSRV that returns fixed records and records what it was
// asked for. Nothing here ever touches DNS.
func answers(records []*net.SRV, err error, asked *string) LookupSRV {
	return func(_ context.Context, service, proto, name string) (string, []*net.SRV, error) {
		*asked = service + "/" + proto + "/" + name
		return "", records, err
	}
}

// The shape Azure DocumentDB actually publishes: one target, port 10260, with
// the trailing root dot DNS always includes.
func azureRecord() []*net.SRV {
	return []*net.SRV{{
		Target: "fc-example-000.global.mongocluster.cosmos.azure.com.",
		Port:   10260,
	}}
}

// The string Azure hands you in the portal, verbatim in shape.
const azurePasted = "mongodb+srv://reader:p%40ssword@cluster-1609.global.mongocluster.cosmos.azure.com/" +
	"?tls=true&authMechanism=SCRAM-SHA-256&retrywrites=false&maxIdleTimeMS=120000"

func TestResolveConvertsWhatAzureGivesYou(t *testing.T) {
	var asked string

	got, problem := Resolve(context.Background(), azurePasted, answers(azureRecord(), nil, &asked))

	if problem != None {
		t.Fatalf("unexpected problem: %q", problem)
	}
	if asked != "mongodb/tcp/cluster-1609.global.mongocluster.cosmos.azure.com" {
		t.Errorf("looked up the wrong record: %q", asked)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("produced an unparseable url %q: %v", got, err)
	}
	if parsed.Scheme != "mongodb" {
		t.Errorf("expected the direct scheme, got %q", parsed.Scheme)
	}
	// The root dot has to go: a driver dialling "host.:10260" does not resolve.
	if parsed.Host != "fc-example-000.global.mongocluster.cosmos.azure.com:10260" {
		t.Errorf("unexpected host: %q", parsed.Host)
	}
	// The credential must survive untouched — still encoded exactly once.
	if password, _ := parsed.User.Password(); password != "p@ssword" {
		t.Errorf("the password did not survive: %q", password)
	}
	if strings.Count(got, "@") != 1 {
		t.Errorf("the password lost its escaping: %q", got)
	}
	// And the options the cluster needs.
	if parsed.Query().Get("authMechanism") != "SCRAM-SHA-256" {
		t.Errorf("the options were lost: %q", parsed.RawQuery)
	}
}

func TestResolveKeepsTLSOnWhenTheSchemeStopsImplyingIt(t *testing.T) {
	// mongodb+srv implies TLS and plain mongodb does not, so a conversion that
	// said nothing would turn an encrypted connection into a refused one — and
	// the error a cluster gives for that mentions neither TLS nor this step.
	var asked string
	bare := "mongodb+srv://user:pass@cluster.example.com/?retrywrites=false"

	got, problem := Resolve(context.Background(), bare, answers(azureRecord(), nil, &asked))

	if problem != None {
		t.Fatalf("unexpected problem: %q", problem)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Query().Get("tls") != "true" {
		t.Errorf("tls was not carried over: %q", got)
	}
}

func TestResolveDoesNotAddTLSWhenItIsAlreadyDecided(t *testing.T) {
	var asked string
	for _, raw := range []string{
		"mongodb+srv://u:p@cluster.example.com/?tls=false",
		"mongodb+srv://u:p@cluster.example.com/?ssl=true",
	} {
		got, problem := Resolve(context.Background(), raw, answers(azureRecord(), nil, &asked))
		if problem != None {
			t.Fatalf("unexpected problem: %q", problem)
		}
		if strings.Contains(got, "tls=true") && strings.Contains(raw, "tls=false") {
			t.Errorf("overrode an explicit tls=false: %q", got)
		}
	}
}

func TestResolveKeepsEverySeedItIsGiven(t *testing.T) {
	// A real replica set publishes several, and the seed list is what lets the
	// driver survive one of them being down.
	var asked string
	records := []*net.SRV{
		{Target: "a.example.com.", Port: 27017},
		{Target: "b.example.com.", Port: 27018},
	}

	got, problem := Resolve(context.Background(), "mongodb+srv://u:p@cluster.example.com/", answers(records, nil, &asked))

	if problem != None {
		t.Fatalf("unexpected problem: %q", problem)
	}
	if !strings.Contains(got, "a.example.com:27017,b.example.com:27018") {
		t.Errorf("expected both seeds, got %q", got)
	}
}

func TestResolveLeavesAnythingThatIsNotSrvAlone(t *testing.T) {
	// Writing the direct form by hand still works exactly as it did — the
	// conversion is one more road in, not a replacement.
	var asked string
	direct := "mongodb://user:pass@host:10260/?tls=true"

	got, problem := Resolve(context.Background(), direct, answers(nil, errors.New("must not be called"), &asked))

	if problem != None || got != direct {
		t.Errorf("expected it untouched, got %q / %q", got, problem)
	}
	if asked != "" {
		t.Errorf("a direct address must not cost a DNS lookup: %q", asked)
	}
}

func TestResolveSaysSoWhenTheRecordIsNotThere(t *testing.T) {
	var asked string
	for _, tc := range []struct {
		name    string
		records []*net.SRV
		err     error
	}{
		{"the lookup failed", nil, errors.New("no such host")},
		{"the lookup answered with nothing", []*net.SRV{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, problem := Resolve(context.Background(), azurePasted, answers(tc.records, tc.err, &asked))
			if problem != SrvUnresolved {
				t.Errorf("expected srvUnresolved, got %q", problem)
			}
		})
	}
}

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

// The reason Build exists. Each of these characters means something inside a
// URL, so a password carrying one has to survive the round trip out the other
// side — which is exactly what typing it into a single address field does not
// do.
func TestBuildSurvivesPasswordsFullOfURLPunctuation(t *testing.T) {
	for _, password := range []string{
		"p@ssword",
		"pa:ss",
		"pa/ss",
		"pa?ss",
		"pa#ss",
		"100%sure",
		"[bracketed]",
		"contraseña",
		"todo junto: @/?#%[]",
		"$ub&del+ims,are;fine=too",
	} {
		built, problem := Build(Credentials{
			Address:  "host:10260/?tls=true",
			User:     "reader",
			Password: password,
		})
		if problem != None {
			t.Errorf("%q was refused: %q", password, problem)
			continue
		}

		// The assertion that actually proves the escaping happened. Round-
		// tripping through url.Parse does not: Go splits userinfo at the LAST
		// `@`, so an unescaped one would come back looking correct here while a
		// MongoDB driver — which splits at the first, per the connection-string
		// spec — read the rest of the password as a hostname.
		if strings.Count(built, "@") != 1 {
			t.Errorf("%q left an unescaped @ in %q", password, built)
		}

		// Read back the way a driver would, rather than comparing to a string
		// this test escaped by hand — which would only prove the test agrees
		// with itself.
		parsed, err := url.Parse(built)
		if err != nil {
			t.Errorf("%q produced an unparseable url %q: %v", password, built, err)
			continue
		}
		got, set := parsed.User.Password()
		if !set || got != password {
			t.Errorf("%q came back as %q (set=%v) from %q", password, got, set, built)
		}
		if parsed.User.Username() != "reader" {
			t.Errorf("the user did not survive %q: %q", password, parsed.User.Username())
		}
		if parsed.Host != "host:10260" {
			t.Errorf("the host did not survive %q: %q", password, parsed.Host)
		}
	}
}

func TestBuildAcceptsAnAddressWithOrWithoutAScheme(t *testing.T) {
	for _, address := range []string{
		"host:10260",
		"mongodb://host:10260",
		"  host:10260  ",
	} {
		built, problem := Build(Credentials{Address: address, User: "u", Password: "p"})
		if problem != None {
			t.Fatalf("%q was refused: %q", address, problem)
		}
		if !strings.HasPrefix(built, "mongodb://u:p@host:10260") {
			t.Errorf("%q built %q", address, built)
		}
	}
}

func TestBuildKeepsTheOptionsThatWereTyped(t *testing.T) {
	// tls=true is not decoration on Azure DocumentDB: without it nothing
	// connects, and it is the one option people paste along with the host.
	built, problem := Build(Credentials{
		Address:  "host:10260/?tls=true&retryWrites=false",
		User:     "u",
		Password: "p",
	})
	if problem != None {
		t.Fatalf("unexpected problem: %q", problem)
	}
	if !strings.HasSuffix(built, "?tls=true&retryWrites=false") {
		t.Errorf("the options were lost: %q", built)
	}
}

func TestBuildWithoutAPasswordIsFine(t *testing.T) {
	// A cluster with no authentication is unusual but legal, and refusing it
	// would be inventing a rule MongoDB does not have.
	built, problem := Build(Credentials{Address: "localhost:27017"})
	if problem != None {
		t.Fatalf("unexpected problem: %q", problem)
	}
	if built != "mongodb://localhost:27017" {
		t.Errorf("unexpected url: %q", built)
	}
}

func TestBuildRefusesTheAmbiguousCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Credentials
		want Problem
	}{
		{
			// Guessing which one wins is how someone spends an evening sure
			// they typed the right password.
			name: "credentials in both places",
			in:   Credentials{Address: "mongodb://other:pass@host", User: "u", Password: "p"},
			want: CredentialsInAddress,
		},
		{
			name: "a password with nobody to go with it",
			in:   Credentials{Address: "host:10260", Password: "p"},
			want: NoUser,
		},
		{
			name: "nothing typed",
			in:   Credentials{User: "u", Password: "p"},
			want: Empty,
		},
		{
			name: "srv, which never connects on windows",
			in:   Credentials{Address: "mongodb+srv://cluster.example.com"},
			want: SRV,
		},
		{
			name: "not mongodb at all",
			in:   Credentials{Address: "https://example.com"},
			want: NotMongo,
		},
		{
			name: "options but no server",
			in:   Credentials{Address: "mongodb:///?tls=true"},
			want: NoHost,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, problem := Build(tc.in); problem != tc.want {
				t.Errorf("expected %q, got %q", tc.want, problem)
			}
		})
	}
}

func TestBuildKeepsSpacesInsideAPassword(t *testing.T) {
	// Trimming the password would be a silent edit of a secret: the failure it
	// causes is an authentication error, with nothing on screen connecting it
	// to a space nobody can see.
	const password = " leading and trailing "
	built, problem := Build(Credentials{Address: "host", User: "u", Password: password})
	if problem != None {
		t.Fatalf("unexpected problem: %q", problem)
	}
	parsed, err := url.Parse(built)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := parsed.User.Password(); got != password {
		t.Errorf("expected %q, got %q", password, got)
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
