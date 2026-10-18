package binding

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// writeBinding lays out a binding directory as plain files, one per key.
func writeBinding(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for key, value := range files {
		if err := os.WriteFile(filepath.Join(dir, key), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// kubeletMount lays out a Secret volume the way the kubelet does: the files
// live in a timestamped directory, ..data is a symlink to it, and each key is
// a symlink through ..data. An update writes a new directory and swaps
// ..data atomically. Call update to do that.
func kubeletMount(t *testing.T, files map[string]string) (dir string, update func(map[string]string)) {
	t.Helper()
	dir = t.TempDir()
	generation := 0
	update = func(files map[string]string) {
		t.Helper()
		generation++
		version := fmt.Sprintf("..2026_10_10_00_00_%02d.%d", generation, generation)
		if err := os.Mkdir(filepath.Join(dir, version), 0o755); err != nil {
			t.Fatal(err)
		}
		for key, value := range files {
			if err := os.WriteFile(filepath.Join(dir, version, key), []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		tmp := filepath.Join(dir, "..data_tmp")
		if err := os.Symlink(version, tmp); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, filepath.Join(dir, "..data")); err != nil {
			t.Fatal(err)
		}
		for key := range files {
			link := filepath.Join(dir, key)
			if _, err := os.Lstat(link); err == nil {
				continue
			}
			if err := os.Symlink(filepath.Join("..data", key), link); err != nil {
				t.Fatal(err)
			}
		}
	}
	update(files)
	return dir, update
}

func validFiles(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		KeyType:     "postgresql",
		KeyHost:     "todo-db.api.svc.cluster.local",
		KeyPort:     "5432",
		KeyDatabase: "todo_db",
		KeyUsername: "app_readwrite",
		KeyPassword: "s3cret pass/with:chars@",
		KeySSLMode:  "verify-full",
		KeyCACert:   testCAPEM(t),
	}
}

func testCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestReadPostgres_ValidBinding(t *testing.T) {
	files := validFiles(t)
	files[KeyHost] = "todo-db.api.svc.cluster.local\n"
	dir := writeBinding(t, files)

	got, err := ReadPostgres(dir)
	if err != nil {
		t.Fatalf("ReadPostgres: %v", err)
	}
	want := Postgres{
		Host:       "todo-db.api.svc.cluster.local",
		Port:       5432,
		Database:   "todo_db",
		Username:   "app_readwrite",
		Password:   "s3cret pass/with:chars@",
		CACertPath: filepath.Join(dir, KeyCACert),
	}
	if got != want {
		t.Fatalf("ReadPostgres = %+v, want %+v", got, want)
	}
}

func TestReadPostgres_KubeletSymlinkLayout(t *testing.T) {
	files := validFiles(t)
	dir, _ := kubeletMount(t, files)

	got, err := ReadPostgres(dir)
	if err != nil {
		t.Fatalf("ReadPostgres: %v", err)
	}
	if got.Host != files[KeyHost] || got.Password != files[KeyPassword] {
		t.Fatalf("ReadPostgres = %+v, want the mounted values", got)
	}
	if _, err := os.ReadFile(got.CACertPath); err != nil {
		t.Fatalf("CACertPath %s is not readable: %v", got.CACertPath, err)
	}
}

// A rotation reaches the pod as a new ..data directory. ReadPassword must
// return the new password on the next call, with no restart.
func TestReadPassword_SeesRotatedSecret(t *testing.T) {
	files := validFiles(t)
	files[KeyPassword] = "old-password"
	dir, update := kubeletMount(t, files)

	if got, err := ReadPassword(dir); err != nil || got != "old-password" {
		t.Fatalf("ReadPassword before rotation = %q, %v; want old-password", got, err)
	}
	files[KeyPassword] = "new-password\n"
	update(files)
	if got, err := ReadPassword(dir); err != nil || got != "new-password\n" {
		t.Fatalf("ReadPassword after rotation = %q, %v; want the new password unchanged", got, err)
	}
}

func TestReadPassword_RejectsMissingOrEmpty(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing", func(f map[string]string) { delete(f, KeyPassword) }},
		{"empty", func(f map[string]string) { f[KeyPassword] = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := validFiles(t)
			tt.mutate(files)
			_, err := ReadPassword(writeBinding(t, files))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("ReadPassword error = %v, want ErrInvalid", err)
			}
		})
	}
}

// os.Root must stop a key symlink from reading outside the binding.
func TestReadPassword_RefusesSymlinkOutsideBinding(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(outside, []byte("not-the-binding"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := validFiles(t)
	delete(files, KeyPassword)
	dir := writeBinding(t, files)
	if err := os.Symlink(outside, filepath.Join(dir, KeyPassword)); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadPassword(dir); err == nil {
		t.Fatalf("ReadPassword followed a symlink out of the binding and returned %q", got)
	}
}

func TestReadPostgres_OptionalKeysMayBeAbsent(t *testing.T) {
	files := validFiles(t)
	delete(files, KeyType)
	delete(files, KeySSLMode)
	if _, err := ReadPostgres(writeBinding(t, files)); err != nil {
		t.Fatalf("ReadPostgres: %v", err)
	}
}

func TestReadPostgres_KeepsPasswordBytes(t *testing.T) {
	files := validFiles(t)
	files[KeyPassword] = " leading and trailing \n"
	got, err := ReadPostgres(writeBinding(t, files))
	if err != nil {
		t.Fatalf("ReadPostgres: %v", err)
	}
	if got.Password != files[KeyPassword] {
		t.Fatalf("password = %q, want it unchanged", got.Password)
	}
}

func TestReadPostgres_RejectsInvalidBindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing host", func(f map[string]string) { delete(f, KeyHost) }},
		{"missing port", func(f map[string]string) { delete(f, KeyPort) }},
		{"missing database", func(f map[string]string) { delete(f, KeyDatabase) }},
		{"missing username", func(f map[string]string) { delete(f, KeyUsername) }},
		{"missing password", func(f map[string]string) { delete(f, KeyPassword) }},
		{"missing ca.crt", func(f map[string]string) { delete(f, KeyCACert) }},
		{"empty host", func(f map[string]string) { f[KeyHost] = " \n" }},
		{"empty password", func(f map[string]string) { f[KeyPassword] = "" }},
		{"empty ca.crt", func(f map[string]string) { f[KeyCACert] = "" }},
		{"non-numeric port", func(f map[string]string) { f[KeyPort] = "postgres" }},
		{"port out of range", func(f map[string]string) { f[KeyPort] = "70000" }},
		{"sslmode disable", func(f map[string]string) { f[KeySSLMode] = "disable" }},
		{"sslmode require", func(f map[string]string) { f[KeySSLMode] = "require" }},
		{"wrong type", func(f map[string]string) { f[KeyType] = "mysql" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := validFiles(t)
			tt.mutate(files)
			_, err := ReadPostgres(writeBinding(t, files))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("ReadPostgres error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestReadPostgres_MissingDirectory(t *testing.T) {
	_, err := ReadPostgres(filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("ReadPostgres succeeded for a directory that does not exist")
	}
}

func TestDir(t *testing.T) {
	tests := []struct {
		root, name string
		want       string
		wantErr    bool
	}{
		{"/bindings", "todo-db-readwrite", "/bindings/todo-db-readwrite", false},
		{"/bindings", "", "", true},
		{"/bindings", "..", "", true},
		{"/bindings", "a/b", "", true},
		{"", "todo-db-readwrite", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.root+"|"+tt.name, func(t *testing.T) {
			got, err := Dir(tt.root, tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Dir error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Dir = %q, want %q", got, tt.want)
			}
		})
	}
}

// The connection string must make pgx verify the server certificate against
// the mounted CA and the host name, with the password intact.
func TestConnString_VerifiesServerWithMountedCA(t *testing.T) {
	p, err := ReadPostgres(writeBinding(t, validFiles(t)))
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := pgx.ParseConfig(p.ConnString("todo-api"))
	if err != nil {
		t.Fatalf("pgx.ParseConfig: %v", err)
	}
	if cfg.Host != p.Host || cfg.Port != uint16(p.Port) || cfg.Database != p.Database ||
		cfg.User != p.Username || cfg.Password != p.Password {
		t.Fatalf("parsed config %s@%s:%d/%s does not match the binding", cfg.User, cfg.Host, cfg.Port, cfg.Database)
	}
	if cfg.RuntimeParams["application_name"] != "todo-api" {
		t.Fatalf("application_name = %q", cfg.RuntimeParams["application_name"])
	}
	tlsCfg := cfg.TLSConfig
	if tlsCfg == nil {
		t.Fatal("TLS is off")
	}
	if tlsCfg.InsecureSkipVerify || tlsCfg.ServerName != p.Host || tlsCfg.RootCAs == nil {
		t.Fatalf("TLS does not verify the server: skip=%v serverName=%q rootCAs=%v",
			tlsCfg.InsecureSkipVerify, tlsCfg.ServerName, tlsCfg.RootCAs != nil)
	}
	// sslmode=verify-full must not fall back to plaintext.
	if len(cfg.Fallbacks) != 0 {
		t.Fatalf("got %d fallback configs, want none", len(cfg.Fallbacks))
	}
}
