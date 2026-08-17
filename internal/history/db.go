// Package history stores every successfully analysed map in a local SQLite
// database, keyed by the SHA-256 of the demo bytes that were parsed, so
// matches deduplicate by content, stored analyses re-render without the
// original demo, and Premier trends recompute exactly from stored additive
// facts. Everything is local: no demo bytes, no absolute demo paths, and no
// network. The package owns the connection, schema, storage, queries and
// trend arithmetic; rendering belongs to the cmd package.
package history

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver, pure Go, CGO-free
)

// EnvDir is the environment variable overriding the history directory.
const EnvDir = "CS2_ANALYSER_HISTORY_DIR"

// dbFileName is the database file inside the history directory. Its WAL and
// shared-memory siblings live next to it, which is why the directory itself
// is created owner-only.
const dbFileName = "history.db"

// DefaultDir resolves the directory holding the history database: EnvDir
// when set, otherwise <user config dir>/cs2-analyser-tool/history. It never
// resolves to the current working directory or the repository.
func DefaultDir() (string, error) {
	return resolveDir(os.Getenv, os.UserConfigDir)
}

// resolveDir is DefaultDir with its environment lookups injected so both
// branches are testable without touching the real user configuration.
func resolveDir(getenv func(string) string, userConfigDir func() (string, error)) (string, error) {
	if dir := getenv(EnvDir); dir != "" {
		return dir, nil
	}
	configDir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving the user config directory for history (set %s to override): %w", EnvDir, err)
	}
	return filepath.Join(configDir, "cs2-analyser-tool", "history"), nil
}

// DB is an open history database.
type DB struct {
	sql  *sql.DB
	path string
}

// applicationID is the PRAGMA application_id stamped into every history
// database ("cs2h" as big-endian ASCII), so a SQLite file some other
// application put at our path is recognized as foreign instead of being
// adopted.
const applicationID = 0x63733268

// poolQuery is the connection configuration of the real pool. The driver
// applies each _-prefixed setting on every physical connection it opens, so
// the pool can never hand out a connection without them. _txlock makes every
// transaction take the write lock at BEGIN, so two processes writing at once
// queue behind the busy timeout instead of deadlocking on a lock upgrade.
// The persistent WAL journal mode is deliberately not here: it is a property
// of the database file, not of a connection, and enableWAL applies it once
// with an explicit retry, because concurrent openers converting the same
// file can hit SQLITE_BUSY.
const poolQuery = "_busy_timeout=5000&_foreign_keys=1&_synchronous=FULL&_txlock=immediate"

// inspectQuery configures the pre-flight inspection connection. It opens the
// private inspection copy, never the real file, so it needs no read-only mode:
// everything opening a SQLite database may do to it — replaying a hot
// rollback journal, rebuilding the WAL index, creating the shared-memory
// file — happens to the copy and is discarded with it. Even mode=ro is not
// genuinely non-writing: it creates the -shm file for a WAL database.
const inspectQuery = "_busy_timeout=5000"

// databaseDSN builds the driver DSN as a file: URI with the path
// percent-encoded, so directory names containing ?, #, % or spaces reach
// SQLite as the literal path instead of part of the query string being split
// off at the first '?'.
func databaseDSN(path, query string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		// Windows drive-letter paths become /C:/..., which SQLite's URI
		// parser converts back.
		slashed = "/" + slashed
	}
	uri := url.URL{Scheme: "file", Path: slashed, RawQuery: query}
	return uri.String()
}

// sqliteHeaderMagic opens every SQLite database file.
const sqliteHeaderMagic = "SQLite format 3\x00"

// sidecarSuffixes are the files SQLite keeps next to a database, in the
// order they are checked for stranding.
var sidecarSuffixes = []string{"-journal", "-wal", "-shm"}

// refuseStrandedSidecars refuses to treat path as a new database while a
// journal sidecar survives beside a blank or missing database file: after an
// interruption or damage the sidecar — a WAL in particular — may hold the
// only surviving copy of the database's contents, and initializing a new
// database there would destroy it. During a healthy concurrent
// initialization the pairing can exist for an instant — converting an empty
// database to WAL journals the header write moments before the header
// appears — so the check re-reads briefly and refuses only a state that
// persists.
func refuseStrandedSidecars(ctx context.Context, path string) error {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		stranded, sidecar, err := strandedSidecar(path)
		if err != nil {
			return err
		}
		if !stranded {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the database file is missing or empty but %s survives beside it and may hold the database's only contents; refusing to initialize a new database over it", sidecar)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// strandedSidecar reports whether the database file is blank or missing
// while one of its sidecars exists, naming the first sidecar found.
func strandedSidecar(path string) (bool, string, error) {
	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, "", fmt.Errorf("checking the database file: %w", err)
	}
	if err == nil && info.Size() > 0 {
		return false, "", nil
	}
	for _, suffix := range sidecarSuffixes {
		name := dbFileName + suffix
		if _, err := os.Stat(path + suffix); err == nil {
			return true, name, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, "", fmt.Errorf("checking for %s: %w", name, err)
		}
	}
	return false, "", nil
}

// headerInfo is what the raw SQLite file header reveals without opening any
// database connection: reading it can never modify the file, its journal or
// its WAL.
type headerInfo struct {
	// blank marks a zero-length file — a brand-new database nothing has
	// written yet, with nothing to protect.
	blank   bool
	version int32
	appID   int32
}

// readHeader reads the 100-byte SQLite header directly from the file. In a
// WAL database the header bytes can lag behind an uncheckpointed write, so
// callers may only refuse on values no history database can ever show, never
// accept on them; inspectDatabase re-reads both fields authoritatively
// through SQL.
func readHeader(path string) (headerInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return headerInfo{}, fmt.Errorf("reading the database header: %w", err)
	}
	defer file.Close()

	header := make([]byte, 100)
	n, err := io.ReadFull(file, header)
	if n == 0 && (err == io.EOF || err == io.ErrUnexpectedEOF) {
		return headerInfo{blank: true}, nil
	}
	if err != nil {
		return headerInfo{}, fmt.Errorf("the file is not a SQLite database (%d-byte header): refusing to modify it", n)
	}
	if string(header[:len(sqliteHeaderMagic)]) != sqliteHeaderMagic {
		return headerInfo{}, fmt.Errorf("the file is not a SQLite database: refusing to modify it")
	}
	return headerInfo{
		version: int32(binary.BigEndian.Uint32(header[60:64])),
		appID:   int32(binary.BigEndian.Uint32(header[68:72])),
	}, nil
}

// inspectDatabase decides whether the file may be used as a history database
// at all. A corrupt file, a blank file with a surviving journal sidecar, a
// database stamped by another application, a nonempty database carrying no
// history schema version, a malformed negative version, a schema newer than
// this build, and a database claiming the current schema without actually
// carrying its tables are all refused before the pool — whose recovery
// processing and persistent WAL conversion modify the file — ever opens it.
//
// It works in two phases, neither of which can modify the inspected
// directory. The raw header alone settles every refusal it can — no
// connection is opened, so even a hot rollback journal stays as it is. What
// the header cannot settle (emptiness, the schema catalog, and values a WAL
// checkpoint lag could be hiding) is read through a real connection against
// a private temporary copy of the database and its journal sidecars, with
// every value taken inside one read transaction so a concurrent initializer
// commits either entirely before or entirely after this snapshot, never in
// the middle of it.
func inspectDatabase(ctx context.Context, path string) error {
	header, err := readHeader(path)
	if err != nil {
		return err
	}
	if header.blank {
		// A blank file is a new database only when no journal sidecar
		// survives beside it. Open already checks this before pre-creating
		// the file; the recheck covers a sidecar that appeared since, and
		// any caller that is not Open.
		return refuseStrandedSidecars(ctx, path)
	}
	if header.version > schemaVersion {
		return errSchemaTooNew(int(header.version))
	}
	if header.version < 0 {
		return errSchemaMalformed(int(header.version))
	}
	// The version and application ID live on the same header page, so a
	// history database's header shows them updated together or not at all;
	// a foreign application ID in the header is therefore never a stale
	// read of one of ours.
	if header.appID != 0 && header.appID != applicationID {
		return errForeignApplication(int(header.appID))
	}
	// The snapshot below is the only authority from here on. In particular,
	// a header the raw read proves ours must NOT shortcut acceptance: a WAL
	// database's header is legitimately stale — a newer version can sit
	// committed but uncheckpointed in the WAL — so only the snapshot's
	// SQL-level read can say what the database really is. If the snapshot
	// cannot be taken or read, the database is refused as it stands rather
	// than opened uninspected.
	state, err := readDatabaseState(ctx, path)
	if err != nil {
		return fmt.Errorf("could not inspect the database on a private copy (%w); refusing to open it uninspected", err)
	}

	if state.version > schemaVersion {
		return errSchemaTooNew(state.version)
	}
	if state.version < 0 {
		return errSchemaMalformed(state.version)
	}
	if state.version == 0 {
		// Version 0 is only ever a database nothing has touched: SQLite's
		// default user_version is also 0, so any existing content means
		// this is some other application's database, not a blank history.
		if state.objects > 0 || (state.appID != 0 && state.appID != applicationID) {
			return fmt.Errorf("the file is not empty and carries no history schema version; refusing to modify a database this tool did not create")
		}
		return nil
	}
	if state.appID != applicationID {
		return errForeignApplication(state.appID)
	}
	// A database claiming the current schema version must actually carry its
	// tables. Refusing here — validateSchema would refuse it anyway — keeps
	// the refusal ahead of the pool, whose persistent WAL conversion would
	// already have modified a database this build then rejects.
	if state.version == schemaVersion && len(state.missingTables) > 0 {
		return errMissingTable(state.missingTables[0])
	}
	return nil
}

// databaseState is the consistent snapshot inspection reads through SQLite.
type databaseState struct {
	version       int
	appID         int
	objects       int      // total sqlite_master entries
	missingTables []string // required current-schema tables absent from the catalog
}

// readDatabaseState reads the schema version, application ID and schema
// catalog from a private copy of the database. The real file and its
// directory are only ever read. The copy is taken without SQLite's locks, so
// a concurrent writer can tear it and fail the read; that is transient, so
// the state is re-snapshotted and re-read briefly before giving up. A failure
// to create the copy at all — no temporary space, an unwritable temporary
// directory — is environmental and reported immediately; the caller then
// refuses rather than opening the database uninspected.
func readDatabaseState(ctx context.Context, path string) (databaseState, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		copyPath, cleanup, err := snapshotForInspection(path)
		if err != nil {
			return databaseState{}, err
		}
		state, err := inspectSnapshot(ctx, copyPath)
		cleanup()
		if err == nil {
			return state, nil
		}
		if time.Now().After(deadline) {
			return databaseState{}, err
		}
		select {
		case <-ctx.Done():
			return databaseState{}, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// inspectSnapshot opens the private copy and reads its state inside one read
// transaction, so the values are a single consistent snapshot even while the
// original keeps changing.
func inspectSnapshot(ctx context.Context, copyPath string) (databaseState, error) {
	db, err := sql.Open("sqlite", databaseDSN(copyPath, inspectQuery))
	if err != nil {
		return databaseState{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return databaseState{}, err
	}
	defer tx.Rollback()
	var state databaseState
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&state.version); err != nil {
		return databaseState{}, fmt.Errorf("reading PRAGMA user_version: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&state.appID); err != nil {
		return databaseState{}, fmt.Errorf("reading PRAGMA application_id: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&state.objects); err != nil {
		return databaseState{}, fmt.Errorf("reading the schema catalog: %w", err)
	}
	present := make(map[string]bool, len(requiredTables))
	rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return databaseState{}, fmt.Errorf("reading the table catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return databaseState{}, fmt.Errorf("reading the table catalog: %w", err)
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return databaseState{}, fmt.Errorf("reading the table catalog: %w", err)
	}
	for _, table := range requiredTables {
		if !present[table] {
			state.missingTables = append(state.missingTables, table)
		}
	}
	return state, nil
}

// snapshotForInspection copies the database and its journal sidecars into a
// fresh owner-only temporary directory for inspectSnapshot to open. The
// shared-memory file is deliberately not copied: it is a rebuildable index
// over the WAL, and SQLite reconstructs it on the copy. The copy is taken
// without SQLite's locks, so a concurrent writer can tear it; that surfaces
// as a read failure on the copy, and readDatabaseState re-snapshots and
// retries briefly before the open is refused outright.
func snapshotForInspection(path string) (copyPath string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "cs2-analyser-history-inspect-")
	if err != nil {
		return "", nil, fmt.Errorf("creating the inspection copy directory: %w", err)
	}
	cleanup = func() { os.RemoveAll(dir) }
	copyPath = filepath.Join(dir, dbFileName)
	if err := copyForInspection(path, copyPath, false); err != nil {
		cleanup()
		return "", nil, err
	}
	for _, suffix := range []string{"-journal", "-wal"} {
		if err := copyForInspection(path+suffix, copyPath+suffix, true); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return copyPath, cleanup, nil
}

// copyForInspection copies src into the private inspection directory,
// owner-only. An optional sidecar that does not exist is simply not part of
// the snapshot.
func copyForInspection(src, dst string, optional bool) error {
	in, err := os.Open(src)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s for inspection: %w", filepath.Base(src), err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating the inspection copy: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copying %s for inspection: %w", filepath.Base(src), err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("writing the inspection copy: %w", err)
	}
	return nil
}

func errSchemaTooNew(version int) error {
	return fmt.Errorf("history database schema version is %d but this build supports at most %d; use a newer cs2-analyser-tool instead of downgrading the database", version, schemaVersion)
}

func errSchemaMalformed(version int) error {
	return fmt.Errorf("history database schema version is %d, which is malformed; refusing to modify a database this build does not understand", version)
}

func errForeignApplication(appID int) error {
	return fmt.Errorf("the database was created by a different application (application_id %#x); refusing to modify it", appID)
}

// requiredTables are the tables a database claiming the current schema
// version must carry. Inspection and validateSchema both refuse on the same
// list with the same error.
var requiredTables = []string{"matches", "match_players", "display_selections", "display_preferences"}

func errMissingTable(table string) error {
	return fmt.Errorf("history database claims schema version %d but has no %q table; refusing to modify a database this build does not understand", schemaVersion, table)
}

// Open opens (creating if needed) the history database inside dir, applies
// the connection settings, and migrates or validates the schema. The
// directory is created owner-only where the platform supports it, which also
// shields the WAL and shared-memory files SQLite keeps next to the database.
// A database this build does not understand — a newer schema, a database
// another application put at this path, a claimed current schema without its
// tables, a blank or missing database beside a surviving WAL or journal, or
// a file that is not a SQLite database — is refused as it stands,
// never deleted, recreated or converted: ownership, version and schema are
// inspected on a private temporary copy before the persistent WAL journal
// mode is ever applied, so a refused directory keeps its exact files and
// bytes.
func Open(ctx context.Context, dir string) (*DB, error) {
	// An explicitly relative override resolves against the working
	// directory exactly once, here — the DSN builder must never guess an
	// absolute location from a relative path.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving history directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating history directory: %w", err)
	}
	path := filepath.Join(dir, dbFileName)
	// A blank or missing database file next to a surviving journal sidecar
	// is refused before the file is even pre-created, so the refusal leaves
	// the directory exactly as found — a missing main file must not gain
	// even an empty one.
	if err := refuseStrandedSidecars(ctx, path); err != nil {
		return nil, fmt.Errorf("opening history database %s: %w", path, err)
	}
	// Pre-create the file owner-only so it never exists with wider
	// permissions, even for an instant; SQLite keeps the mode of an
	// existing file.
	handle, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating history database %s: %w", path, err)
	}
	handle.Close()

	if err := inspectDatabase(ctx, path); err != nil {
		return nil, fmt.Errorf("opening history database %s: %w", path, err)
	}
	// The file is accepted as ours (or blank), so owner-only permissions
	// can be enforced even when it pre-existed with a wider mode —
	// OpenFile's mode applies only to files it creates. A refused file is
	// never touched, chmod included.
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("restricting history database %s to its owner: %w", path, err)
	}

	sqlDB, err := sql.Open("sqlite", databaseDSN(path, poolQuery))
	if err != nil {
		return nil, fmt.Errorf("opening history database %s: %w", path, err)
	}
	// A deliberately minimal pool: the CLI is a single writer and its reads
	// are sequential, so one connection removes in-process lock contention
	// outright. Concurrency across processes is SQLite's job, governed by
	// the busy timeout.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	db := &DB{sql: sqlDB, path: path}
	if err := db.enableWAL(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening history database %s: %w", path, err)
	}
	if err := db.verifySettings(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening history database %s: %w", path, err)
	}
	if err := db.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening history database %s: %w", path, err)
	}
	return db, nil
}

// enableWAL switches the database's persistent journal mode to WAL. The
// conversion needs locks a concurrent opener converting the same file can
// hold, and SQLite reports that as SQLITE_BUSY rather than waiting the full
// busy timeout, so the conversion is retried until it succeeds or its own
// timeout expires. Once any opener has converted the file, the pragma is a
// no-op for everyone after.
func (db *DB) enableWAL(ctx context.Context) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		var mode string
		err := db.sql.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode)
		if err == nil && strings.EqualFold(mode, "wal") {
			return nil
		}
		if time.Now().After(deadline) {
			if err == nil {
				err = fmt.Errorf("journal mode stayed %q", mode)
			}
			return fmt.Errorf("enabling the WAL journal mode: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("enabling the WAL journal mode: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// Close releases the database handle.
func (db *DB) Close() error {
	return db.sql.Close()
}

// verifySettings reads back every required connection setting rather than
// trusting the DSN: a driver change that silently dropped one would corrupt
// durability or integrity guarantees, so mis-verification is an open error.
// It runs only after inspectDatabase has accepted the file as ours (or
// blank), because the pool's journal_mode=WAL persistently converts the
// database it touches.
func (db *DB) verifySettings(ctx context.Context) error {
	checks := []struct{ pragma, want string }{
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
		{"journal_mode", "wal"},
		{"synchronous", "2"}, // 2 is FULL
	}
	for _, check := range checks {
		var got string
		if err := db.sql.QueryRowContext(ctx, "PRAGMA "+check.pragma).Scan(&got); err != nil {
			return fmt.Errorf("reading PRAGMA %s: %w", check.pragma, err)
		}
		if !strings.EqualFold(got, check.want) {
			return fmt.Errorf("PRAGMA %s = %q, want %q", check.pragma, got, check.want)
		}
	}
	return nil
}

// schemaVersion is the newest schema this build understands, recorded in
// PRAGMA user_version.
const schemaVersion = 1

// migrations[v] upgrades a database from user_version v to v+1 inside the
// transaction it is handed; runMigration records the new version in that
// same transaction, so a failed step leaves the file exactly at the version
// it started from. A future schema 2 adds its step as migrations[1].
var migrations = []func(context.Context, *sql.Tx) error{
	0: createSchema1,
}

// migrate brings the database to schemaVersion: version 0 creates schema 1,
// an already-current version is validated and kept, and a newer version is
// refused — downgrading or recreating a database this build does not
// understand would destroy history it cannot even read. Each pending
// migration re-reads the version once it holds the write lock, so several
// processes initializing the same database queue instead of colliding, and
// the loop re-reads after every attempt rather than assuming it applied.
func (db *DB) migrate(ctx context.Context) error {
	for {
		version, err := db.userVersion(ctx)
		if err != nil {
			return err
		}
		if version > schemaVersion {
			return errSchemaTooNew(version)
		}
		// The negative guard keeps a malformed version a clean refusal
		// rather than an out-of-range index into the migration table, even
		// when migrate is reached without Open's inspection.
		if version < 0 {
			return errSchemaMalformed(version)
		}
		if version == schemaVersion {
			return db.validateSchema(ctx)
		}
		if err := db.runMigration(ctx, version); err != nil {
			return fmt.Errorf("migrating history schema from version %d to %d: %w", version, version+1, err)
		}
	}
}

func (db *DB) userVersion(ctx context.Context) (int, error) {
	var version int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("reading PRAGMA user_version: %w", err)
	}
	return version, nil
}

// runMigration applies the from → from+1 step. The version is re-read once
// the immediate transaction holds the write lock: a concurrent process may
// have applied the same step between the caller's read and this lock, in
// which case this is a no-op and the caller's loop re-reads the new state.
func (db *DB) runMigration(ctx context.Context, from int) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("re-reading PRAGMA user_version under the write lock: %w", err)
	}
	if current != from {
		return nil
	}
	if err := migrations[from](ctx, tx); err != nil {
		return err
	}
	// PRAGMA cannot take a bound parameter; the version is the loop
	// counter, never external input.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", from+1)); err != nil {
		return fmt.Errorf("recording user_version %d: %w", from+1, err)
	}
	return tx.Commit()
}

// createSchema1 creates the initial schema.
//
// matches is one immutable row per analysed map, keyed by the demo content
// digest. analysed_at is the UTC RFC3339Nano analysis time — when the demo
// was parsed, not when the match was played, which no demo records
// trustworthily. analysis_json is the complete unfiltered MapAnalysis in its
// stable four-key envelope.
//
// match_players is one row per participating player with the canonical
// unsigned-decimal SteamID64, that map's alias observation, the complete
// DemoPlayer, and the exact aggregation facts trends are recomputed from.
//
// display_selections records the matches whose display was explicitly
// chosen, and display_preferences holds the chosen SteamIDs. No
// display_selections row means everyone; a row with zero preference rows is
// an explicit selection that keeps no player visible in this map — a series
// selection of players who all sat this map out. Preferences cascade from
// match_players so they can only name players actually in the map.
func createSchema1(ctx context.Context, tx *sql.Tx) error {
	const schema = `
CREATE TABLE matches (
	demo_sha256      TEXT PRIMARY KEY
		CHECK (length(demo_sha256) = 64 AND demo_sha256 NOT GLOB '*[^0-9a-f]*'),
	analysed_at      TEXT NOT NULL CHECK (analysed_at <> ''),
	analysis_version TEXT NOT NULL,
	map_name         TEXT NOT NULL,
	game_mode        TEXT NOT NULL,
	score_kind       TEXT NOT NULL CHECK (score_kind IN ('teams', 'sides')),
	score_a          INTEGER NOT NULL CHECK (score_a >= 0),
	score_b          INTEGER NOT NULL CHECK (score_b >= 0),
	analysis_json    BLOB NOT NULL
) STRICT;

CREATE INDEX matches_by_analysed_at ON matches (analysed_at DESC, demo_sha256);

CREATE INDEX matches_by_game_mode ON matches (game_mode);

CREATE TABLE match_players (
	match_sha256 TEXT NOT NULL,
	steam_id     TEXT NOT NULL
		CHECK (steam_id GLOB '[1-9]*' AND steam_id NOT GLOB '*[^0-9]*'),
	alias        TEXT NOT NULL,
	player_json  BLOB NOT NULL,
	facts_json   BLOB NOT NULL,
	PRIMARY KEY (match_sha256, steam_id),
	FOREIGN KEY (match_sha256)
		REFERENCES matches (demo_sha256)
		ON DELETE CASCADE
) STRICT;

CREATE INDEX match_players_by_steam_id ON match_players (steam_id);

CREATE TABLE display_selections (
	match_sha256 TEXT PRIMARY KEY,
	FOREIGN KEY (match_sha256)
		REFERENCES matches (demo_sha256)
		ON DELETE CASCADE
) STRICT;

CREATE TABLE display_preferences (
	match_sha256 TEXT NOT NULL,
	steam_id     TEXT NOT NULL,
	PRIMARY KEY (match_sha256, steam_id),
	FOREIGN KEY (match_sha256, steam_id)
		REFERENCES match_players (match_sha256, steam_id)
		ON DELETE CASCADE
) STRICT;
`
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("creating schema 1: %w", err)
	}
	// The application ID marks the file as ours in the same transaction
	// that creates the schema, so no committed state is ever unclaimed.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id = %d", applicationID)); err != nil {
		return fmt.Errorf("recording the application ID: %w", err)
	}
	return nil
}

// validateSchema confirms a database claiming the current version actually
// carries its tables. A file whose user_version says 1 but whose schema is
// something else entirely is refused, never repaired or recreated. Open's
// inspection already refuses this before the pool; this backstop covers the
// paths that reach migrate without it.
func (db *DB) validateSchema(ctx context.Context) error {
	for _, table := range requiredTables {
		var name string
		err := db.sql.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err == sql.ErrNoRows {
			return errMissingTable(table)
		}
		if err != nil {
			return fmt.Errorf("validating history schema: %w", err)
		}
	}
	return nil
}
