// Package binding reads PostgreSQL connection details from a Kupe database
// binding Secret mounted as a volume.
//
// The Secret follows the servicebinding.io layout: one file per key, mounted
// at $SERVICE_BINDING_ROOT/<name>. Files rather than environment variables,
// because TLS verification needs ca.crt on disk (sslrootcert is a path), the
// Secret's uri key names that mount path, and the kubelet refreshes mounted
// files when the Secret changes, so ReadPassword sees a rotated password
// without a restart.
package binding

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Keys in a Kupe PostgreSQL binding Secret.
const (
	KeyType     = "type"
	KeyHost     = "host"
	KeyPort     = "port"
	KeyDatabase = "database"
	KeyUsername = "username"
	KeyPassword = "password" //#nosec G101 -- the name of a file in the binding, not a credential
	KeySSLMode  = "sslmode"
	KeyCACert   = "ca.crt"
)

const (
	bindingType = "postgresql"
	sslMode     = "verify-full"
)

// ErrInvalid is returned for a binding that is missing a key or holds a value
// this application refuses to use.
var ErrInvalid = errors.New("invalid database binding")

// Postgres is the connection detail of one PostgreSQL binding.
type Postgres struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	// CACertPath is the mounted ca.crt file. The server certificate is
	// verified against it, and its host name against Host.
	CACertPath string
}

// Dir returns the directory a binding is mounted at, refusing a name that
// would escape the binding root.
func Dir(root, name string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("%w: binding root is empty", ErrInvalid)
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%w: binding name %q must be a single path element", ErrInvalid, name)
	}
	return filepath.Join(root, name), nil
}

// ReadPostgres reads and validates the binding mounted at dir.
//
// The password is used exactly as stored. Every other value has surrounding
// whitespace removed, so a Secret written by hand with a trailing newline
// still works.
func ReadPostgres(dir string) (Postgres, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Postgres{}, fmt.Errorf("open binding %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	values := map[string]string{}
	for _, key := range []string{KeyHost, KeyPort, KeyDatabase, KeyUsername, KeyPassword} {
		if values[key], err = readKey(root, key, true); err != nil {
			return Postgres{}, err
		}
	}
	for _, key := range []string{KeyType, KeySSLMode} {
		if values[key], err = readKey(root, key, false); err != nil {
			return Postgres{}, err
		}
	}
	for key, value := range values {
		if key != KeyPassword {
			values[key] = strings.TrimSpace(value)
		}
	}

	if t := values[KeyType]; t != "" && t != bindingType {
		return Postgres{}, fmt.Errorf("%w: type is %q, want %q", ErrInvalid, t, bindingType)
	}
	// TLS with full verification is not negotiable: refuse a binding that asks
	// for anything weaker rather than quietly upgrading or downgrading it.
	if mode := values[KeySSLMode]; mode != "" && mode != sslMode {
		return Postgres{}, fmt.Errorf("%w: sslmode is %q, only %q is supported", ErrInvalid, mode, sslMode)
	}
	for _, key := range []string{KeyHost, KeyDatabase, KeyUsername, KeyPassword} {
		if values[key] == "" {
			return Postgres{}, fmt.Errorf("%w: key %q is empty", ErrInvalid, key)
		}
	}
	port, err := strconv.Atoi(values[KeyPort])
	if err != nil || port < 1 || port > 65535 {
		return Postgres{}, fmt.Errorf("%w: port %q is not a TCP port", ErrInvalid, values[KeyPort])
	}

	ca, err := root.Stat(KeyCACert)
	if err != nil {
		return Postgres{}, fmt.Errorf("%w: key %q: %w", ErrInvalid, KeyCACert, err)
	}
	if ca.Size() == 0 {
		return Postgres{}, fmt.Errorf("%w: key %q is empty", ErrInvalid, KeyCACert)
	}

	return Postgres{
		Host:       values[KeyHost],
		Port:       port,
		Database:   values[KeyDatabase],
		Username:   values[KeyUsername],
		Password:   values[KeyPassword],
		CACertPath: filepath.Join(dir, KeyCACert),
	}, nil
}

// ReadPassword reads only the password from the binding mounted at dir,
// exactly as stored. The API calls it for every new database connection so a
// rotated password is used as soon as the kubelet updates the mounted Secret.
func ReadPassword(dir string) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("open binding %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	password, err := readKey(root, KeyPassword, true)
	if err != nil {
		return "", err
	}
	if password == "" {
		return "", fmt.Errorf("%w: key %q is empty", ErrInvalid, KeyPassword)
	}
	return password, nil
}

// readKey reads one key through root, so a key cannot name a file outside the
// binding. Symlinks that stay inside it, like the kubelet's ..data layout,
// are followed.
func readKey(root *os.Root, key string, required bool) (string, error) {
	raw, err := root.ReadFile(key)
	if errors.Is(err, fs.ErrNotExist) {
		if required {
			return "", fmt.Errorf("%w: key %q is missing from %s", ErrInvalid, key, root.Name())
		}
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read binding key %q: %w", key, err)
	}
	return string(raw), nil
}

// ConnString returns a libpq-style URL that verifies the server certificate
// against the binding's CA and the server host name against Host. It contains
// the password: never log it.
func (p Postgres) ConnString(applicationName string) string {
	query := url.Values{}
	query.Set("sslmode", sslMode)
	query.Set("sslrootcert", p.CACertPath)
	if applicationName != "" {
		query.Set("application_name", applicationName)
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.Username, p.Password),
		Host:     net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:     "/" + p.Database,
		RawQuery: query.Encode(),
	}
	return u.String()
}
