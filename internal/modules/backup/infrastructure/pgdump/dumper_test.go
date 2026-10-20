package pgdump

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("PGDUMP_TEST_HELPER") == "1" {
		if slices.Contains(os.Args, "--version") {
			if os.Getenv("PGDUMP_TEST_FAIL_VERSION") == "1" {
				fmt.Fprint(os.Stderr, os.Getenv("PGPASSWORD"))
				os.Exit(2)
			}
			fmt.Println(os.Getenv("PGDUMP_TEST_VERSION"))
			os.Exit(0)
		}
		if slices.Contains(os.Args, "--command") {
			if os.Getenv("PGDUMP_TEST_PROBE_SLEEP") == "1" {
				time.Sleep(10 * time.Second)
			}
			if os.Getenv("PGDUMP_TEST_PROBE_FAIL") == "1" {
				fmt.Fprint(os.Stderr, os.Getenv("PGPASSWORD"))
				os.Exit(2)
			}
			data, _ := json.Marshal(struct {
				Args     []string
				Password string
			}{os.Args[1:], os.Getenv("PGPASSWORD")})
			if err := os.WriteFile(os.Getenv("PGDUMP_TEST_PROBE_CAPTURE"), data, 0600); err != nil {
				os.Exit(3)
			}
			fmt.Println(os.Getenv("PGDUMP_TEST_SERVER_VERSION"))
			os.Exit(0)
		}
		if os.Getenv("PGDUMP_TEST_SLEEP") == "1" {
			time.Sleep(10 * time.Second)
		}
		if os.Getenv("PGDUMP_TEST_FAIL") == "1" {
			password := os.Getenv("PGPASSWORD")
			fmt.Fprint(os.Stderr, password+" "+url.QueryEscape(password)+" "+strings.Repeat("x", diagnosticLimit*2))
			os.Exit(2)
		}
		capture := struct {
			Args     []string
			Password string
		}{os.Args[1:], os.Getenv("PGPASSWORD")}
		data, _ := json.Marshal(capture)
		if err := os.WriteFile(os.Getenv("PGDUMP_TEST_CAPTURE"), data, 0600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperDumper(t *testing.T, version, dsn string) *Dumper {
	t.Helper()
	t.Setenv("PGDUMP_TEST_HELPER", "1")
	t.Setenv("PGDUMP_TEST_VERSION", "pg_dump (PostgreSQL) "+version)
	t.Setenv("PGDUMP_TEST_CAPTURE", filepath.Join(t.TempDir(), "capture.json"))
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	d := New(dsn, binary)
	d.probe = func(context.Context, string) (string, int, error) { return "15.10", 150010, nil }
	return d
}

func TestPrepareDSN(t *testing.T) {
	cases := []struct {
		name, dsn, password string
		keep                []string
	}{
		{"url", "postgres://admin:p%40ss%20%27%5C@localhost/db?sslmode=verify-full&sslrootcert=%2Fcerts%2Froot.pem&sslsni=0&statement_cache_capacity=0", "p@ss '\\", []string{"sslmode=verify-full", "sslrootcert=%2Fcerts%2Froot.pem", "sslsni=0"}},
		{"url query", "postgresql://admin:old@localhost/db?password=new%2Bpass&sslmode=require&default_query_exec_mode=simple_protocol", "new+pass", []string{"sslmode=require"}},
		{"keyword", `host=localhost user=admin password='p a\'ss\\word' dbname=db sslmode=verify-full sslrootcert='/certs/root cert.pem' sslcert=/certs/client.pem sslkey=/certs/client.key description_cache_capacity=0`, "p a'ss\\word", []string{"sslmode=verify-full", "sslrootcert='/certs/root cert.pem'", "sslcert=/certs/client.pem", "sslkey=/certs/client.key"}},
		{"escaped space", `host=localhost password=p\ a\ ss dbname=db`, "p a ss", []string{"dbname=db"}},
		{"empty", "host=localhost password='' dbname=db\n", "", []string{"dbname=db"}},
		{"duplicate", "password=first password=second database=db", "second", []string{"dbname=db"}},
		{"URL duplicate", "postgres://admin@localhost/db?password=first&password=second", "first", []string{"localhost/db"}},
		{"unquoted apostrophe", "password=p'ass host=localhost", "p'ass", []string{"host=localhost"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := prepareDSN(tc.dsn)
			if err != nil {
				t.Fatal(err)
			}
			if out.env["PGPASSWORD"] != tc.password {
				t.Fatal("incorrect password environment")
			}
			if strings.Contains(out.dsn, "password") || strings.Contains(out.dsn, "cache_capacity") || strings.Contains(out.dsn, "default_query_exec_mode") {
				t.Fatalf("credentials or pgx parameters retained: %q", out.dsn)
			}
			for _, option := range tc.keep {
				if !strings.Contains(out.dsn, option) {
					t.Errorf("lost libpq option %q: %q", option, out.dsn)
				}
			}
		})
	}
	for _, dsn := range []string{`host=localhost password='unterminated`, `password=abc\`, `password='abc'host=db`, `host`, "postgres://user:p%ZZ@db/x", "postgres://user:secret@db/x?bad=%ZZ"} {
		if _, err := prepareDSN(dsn); err == nil {
			t.Error("accepted malformed DSN")
		}
	}
}

func TestSSLPasswordAndServiceFileOnlyInEnvironment(t *testing.T) {
	for _, dsn := range []string{
		`host=localhost password=db-secret sslpassword='tls secret' sslmode=verify-full ssl_min_protocol_version=TLSv1.2 ssl_max_protocol_version=TLSv1.3 sslcrl=/certs/root.crl servicefile='/path with spaces/pg_service.conf'`,
		`postgres://user:db-secret@localhost/db?sslpassword=tls+secret&sslmode=verify-full&ssl_min_protocol_version=TLSv1.2&ssl_max_protocol_version=TLSv1.3&sslcrl=%2Fcerts%2Froot.crl&servicefile=%2Fpath+with+spaces%2Fpg_service.conf`,
	} {
		out, err := prepareDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if out.env["PGPASSWORD"] != "db-secret" || out.env["PGSSLPASSWORD"] != "tls secret" || out.env["PGSERVICEFILE"] != "/path with spaces/pg_service.conf" {
			t.Fatal("connection secrets or service file were not moved to the environment")
		}
		for _, parameter := range []string{"sslmode=verify-full", "ssl_min_protocol_version=TLSv1.2", "ssl_max_protocol_version=TLSv1.3", "sslcrl="} {
			if !strings.Contains(out.dsn, parameter) {
				t.Fatalf("lost SSL parameter %s", parameter)
			}
		}
		if strings.Contains(out.dsn, "secret") || strings.Contains(out.dsn, "servicefile") {
			t.Fatal("connection secrets retained in arguments")
		}
	}
}

func TestCheckMissingAndFailingTool(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		d := New("password=secret", filepath.Join(t.TempDir(), "missing-pg-dump"))
		if _, err := d.Check(context.Background()); err == nil {
			t.Fatal("missing tool accepted")
		}
	})
	t.Run("failure", func(t *testing.T) {
		d := helperDumper(t, "15.10", "password=secret")
		t.Setenv("PGDUMP_TEST_FAIL_VERSION", "1")
		t.Setenv("PGPASSWORD", "secret")
		if _, err := d.Check(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("version tool failure was not redacted")
		}
	})
}

func helperLibpqDumper(t *testing.T) *Dumper {
	t.Helper()
	d := helperDumper(t, "15.19", `host=localhost password='libpq secret' sslmode=verify-full ssl_min_protocol_version=TLSv1.2 sslcrl=/certs/root.crl`)
	directory := t.TempDir()
	for _, name := range []string{"pg_dump", "psql"} {
		if err := os.Symlink(d.path, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	d.path = filepath.Join(directory, "pg_dump")
	d.probe = d.serverVersion
	t.Setenv("PGDUMP_TEST_PROBE_CAPTURE", filepath.Join(t.TempDir(), "probe.json"))
	t.Setenv("PGDUMP_TEST_SERVER_VERSION", "15.19|150019")
	return d
}

func TestCheckUsesSameLibpqOptionsForServerProbe(t *testing.T) {
	d := helperLibpqDumper(t)
	info, err := d.Check(context.Background())
	if err != nil || info.PgDumpVersion != "15.19" || info.ServerVersion != "15.19" {
		t.Fatalf("incorrect libpq version check: %+v, %v", info, err)
	}
	data, err := os.ReadFile(os.Getenv("PGDUMP_TEST_PROBE_CAPTURE"))
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Args     []string
		Password string
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(capture.Args, " ")
	if capture.Password != "libpq secret" || strings.Contains(args, "secret") || strings.Contains(args, "password=") {
		t.Fatal("server probe exposed the database password")
	}
	for _, argument := range []string{"--no-psqlrc", "--no-password", "ON_ERROR_STOP=1", "ssl_min_protocol_version=TLSv1.2", "sslcrl=/certs/root.crl"} {
		if !strings.Contains(args, argument) {
			t.Errorf("server probe lost %s", argument)
		}
	}
}

func TestServerProbeFailuresAndCancellation(t *testing.T) {
	for _, response := range []string{"", "15.19|invalid", "15.19|-1", strings.Repeat("x", diagnosticLimit) + "|150019"} {
		t.Run("invalid response", func(t *testing.T) {
			d := helperLibpqDumper(t)
			t.Setenv("PGDUMP_TEST_SERVER_VERSION", response)
			if _, err := d.Check(context.Background()); err == nil {
				t.Fatal("accepted an invalid server version")
			}
		})
	}
	t.Run("failure", func(t *testing.T) {
		d := helperLibpqDumper(t)
		t.Setenv("PGDUMP_TEST_PROBE_FAIL", "1")
		if _, _, err := d.serverVersion(context.Background(), d.dsn); err == nil || strings.Contains(err.Error(), "libpq secret") {
			t.Fatal("server probe stderr was not redacted")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		d := helperLibpqDumper(t)
		t.Setenv("PGDUMP_TEST_PROBE_SLEEP", "1")
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if _, err := d.Check(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("server probe did not propagate cancellation: %v", err)
		}
	})
}

func TestDumpPasswordOnlyInEnvironmentAndCustomFormat(t *testing.T) {
	d := helperDumper(t, "16.4", "postgres://admin:secret%20%24%28echo%20x%29@localhost/db?sslmode=require")
	path := filepath.Join(t.TempDir(), "backup ; $(touch sentinel).dump")
	info, err := d.Dump(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if info.PgDumpVersion != "16.4" || info.ServerVersion != "15.10" {
		t.Fatalf("wrong versions: %+v", info)
	}
	data, err := os.ReadFile(os.Getenv("PGDUMP_TEST_CAPTURE"))
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Args     []string
		Password string
	}
	if err = json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if capture.Password != "secret $(echo x)" {
		t.Fatal("missing password in child environment")
	}
	if strings.Contains(strings.Join(capture.Args, " "), "secret") || !slices.Contains(capture.Args, "--format=custom") || !slices.Contains(capture.Args, "--no-password") || !slices.Contains(capture.Args, path) {
		t.Fatal("unsafe or incorrect pg_dump arguments")
	}
}

func TestCheckRejectsOldClientAndInvalidVersions(t *testing.T) {
	for _, version := range []string{"14.13", "9.6.24", "invalid"} {
		t.Run(version, func(t *testing.T) {
			d := helperDumper(t, version, "host=localhost password=secret sslmode=disable")
			if _, err := d.Dump(context.Background(), "unused"); err == nil {
				t.Fatal("expected incompatible version error")
			}
			if _, err := os.Stat(os.Getenv("PGDUMP_TEST_CAPTURE")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("dump ran with an incompatible client")
			}
		})
	}
	d := helperDumper(t, "15.10", "password=secret")
	d.probe = func(context.Context, string) (string, int, error) {
		return "", 0, errors.New("connection failed: password=secret")
	}
	if _, err := d.Check(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("server version failure is not redacted")
	}
}

func TestDumpCancellationAndBoundedRedactedStderr(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		d := helperDumper(t, "15.10", "password=secret")
		t.Setenv("PGDUMP_TEST_SLEEP", "1")
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		if _, err := d.Dump(ctx, "unused"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline, got %v", err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("subprocess was not cancelled promptly")
		}
	})
	t.Run("stderr", func(t *testing.T) {
		d := helperDumper(t, "15.10", "password='super secret'")
		t.Setenv("PGDUMP_TEST_FAIL", "1")
		_, err := d.Dump(context.Background(), "unused")
		if err == nil || strings.Contains(err.Error(), "super secret") || strings.Contains(err.Error(), "super+secret") || len(err.Error()) > diagnosticLimit+200 || !strings.Contains(err.Error(), "truncated") {
			t.Fatal("stderr is not bounded and redacted")
		}
	})
}

func TestStderrBoundaryRedaction(t *testing.T) {
	output := &boundedOutput{}
	_, _ = output.Write([]byte(strings.Repeat("x", diagnosticLimit-5) + "secret-value"))
	err := commandError(context.Background(), "export", errors.New("exit"), output, []string{"secret-value"})
	if strings.Contains(err.Error(), "secre") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatal("partial password leaked at stderr limit")
	}
}

func TestIntegrationPostgres(t *testing.T) {
	dsn := os.Getenv("BACKUP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set BACKUP_TEST_POSTGRES_DSN and optionally BACKUP_TEST_PG_DUMP for a real PostgreSQL export")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	d := New(dsn, os.Getenv("BACKUP_TEST_PG_DUMP"))
	path := filepath.Join(t.TempDir(), "backup.dump")
	info, err := d.Dump(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 5 || string(data[:5]) != "PGDMP" {
		t.Fatal("pg_dump did not produce a custom archive")
	}
	if info.PgDumpVersion == "" || info.ServerVersion == "" {
		t.Fatal("missing real PostgreSQL versions")
	}
	t.Run("libpq-only options", func(t *testing.T) {
		probeDSN := dsn
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatal("invalid integration DSN")
			}
			query := u.Query()
			query.Set("ssl_min_protocol_version", "TLSv1.2")
			query.Set("keepalives", "1")
			u.RawQuery = query.Encode()
			probeDSN = u.String()
		} else {
			probeDSN += " ssl_min_protocol_version=TLSv1.2 keepalives=1"
		}
		if _, err := New(probeDSN, d.path).Check(ctx); err != nil {
			t.Fatalf("libpq-only options failed: %v", err)
		}
	})
	// pg_restore only lists this archive; the integration test never changes data.
	restore := filepath.Join(filepath.Dir(d.path), "pg_restore")
	if d.path == "pg_dump" {
		restore = "pg_restore"
	}
	if _, err := exec.CommandContext(ctx, restore, "--list", path).Output(); err != nil {
		t.Fatalf("pg_restore could not inspect the custom archive: %v", err)
	}
}
