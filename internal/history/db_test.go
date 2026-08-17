package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// openTestDB opens a history database in its own temporary directory, never
// the real user database, and closes it with the test.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	return openTestDBAt(t, t.TempDir())
}

func openTestDBAt(t *testing.T, dir string) *DB {
	t.Helper()
	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestResolveDirPrefersEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "elsewhere")
	dir, err := resolveDir(
		func(key string) string {
			if key != EnvDir {
				t.Errorf("looked up %q, want %q", key, EnvDir)
			}
			return override
		},
		func() (string, error) {
			t.Error("user config dir consulted despite the override")
			return "", nil
		},
	)
	if err != nil || dir != override {
		t.Fatalf("resolveDir = %q, %v; want the override %q", dir, err, override)
	}
}

func TestResolveDirDefaultsToUserConfig(t *testing.T) {
	dir, err := resolveDir(
		func(string) string { return "" },
		func() (string, error) { return "/home/someone/.config", nil },
	)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	want := filepath.Join("/home/someone/.config", "cs2-analyser-tool", "history")
	if dir != want {
		t.Errorf("resolveDir = %q, want %q", dir, want)
	}
}

func TestResolveDirReportsConfigFailure(t *testing.T) {
	configErr := errors.New("no home")
	_, err := resolveDir(func(string) string { return "" }, func() (string, error) { return "", configErr })
	if !errors.Is(err, configErr) {
		t.Fatalf("err = %v, want the config error's identity", err)
	}
	if !strings.Contains(err.Error(), EnvDir) {
		t.Errorf("error %q does not mention the %s override", err, EnvDir)
	}
}

func TestDefaultDirUsesEnvironmentOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "hist")
	t.Setenv(EnvDir, override)
	dir, err := DefaultDir()
	if err != nil || dir != override {
		t.Fatalf("DefaultDir = %q, %v; want %q", dir, err, override)
	}
}

// TestOpenCreatesSchemaAndVerifiesSettings pins the initial migration and
// every required connection setting, then reopens the same file to pin that
// an existing schema-1 database validates and continues.
func TestOpenCreatesSchemaAndVerifiesSettings(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openTestDBAt(t, dir)

	version, err := db.userVersion(ctx)
	if err != nil || version != 1 {
		t.Fatalf("user_version = %d, %v; want 1", version, err)
	}
	for pragma, want := range map[string]string{
		"foreign_keys": "1",
		"busy_timeout": "5000",
		"journal_mode": "wal",
		"synchronous":  "2",
	} {
		var got string
		if err := db.sql.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if !strings.EqualFold(got, want) {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	reopened := openTestDBAt(t, dir)
	if version, err := reopened.userVersion(ctx); err != nil || version != 1 {
		t.Fatalf("reopened user_version = %d, %v; want 1", version, err)
	}
	if _, err := reopened.ListMatches(ctx); err != nil {
		t.Fatalf("listing after reopen: %v", err)
	}
}

// seedDatabase runs raw statements against the database file with plain
// driver defaults, planting states Open must judge.
func seedDatabase(t *testing.T, dir string, statements ...string) {
	t.Helper()
	seed, err := openBare(dir)
	if err != nil {
		t.Fatalf("opening bare connection: %v", err)
	}
	defer seed.Close()
	for _, statement := range statements {
		if _, err := seed.Exec(statement); err != nil {
			t.Fatalf("seeding %q: %v", statement, err)
		}
	}
}

// directoryState reads every file in dir by name and content, so a test can
// prove a refused open left the complete directory untouched: the same files,
// their exact bytes, and no new sidecars such as a WAL shared-memory file.
func directoryState(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing %s: %v", dir, err)
	}
	state := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		state[entry.Name()] = string(data)
	}
	return state
}

// requireDirectoryUnchanged asserts the directory still holds exactly the
// files of the before snapshot with their exact bytes.
func requireDirectoryUnchanged(t *testing.T, dir string, before map[string]string) {
	t.Helper()
	after := directoryState(t, dir)
	if maps.Equal(before, after) {
		return
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("refusal created %s", name)
		}
	}
	for name, content := range before {
		got, ok := after[name]
		if !ok {
			t.Errorf("refusal removed %s", name)
			continue
		}
		if got != content {
			t.Errorf("refusal changed the bytes of %s", name)
		}
	}
}

// rawState reads user_version and the persistent journal mode without going
// through Open, to prove a refused database was left untouched.
func rawState(t *testing.T, dir string) (version int, journalMode string) {
	t.Helper()
	inspect, err := openBare(dir)
	if err != nil {
		t.Fatalf("opening bare connection: %v", err)
	}
	defer inspect.Close()
	if err := inspect.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("reading raw user_version: %v", err)
	}
	if err := inspect.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("reading raw journal_mode: %v", err)
	}
	return version, journalMode
}

// TestOpenRefusesNewerSchema pins the forward-compatibility rule: a database
// from a newer build errors clearly and is never downgraded — nor converted
// to WAL, which would already be a modification of a database this build
// refused.
func TestOpenRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	seedDatabase(t, dir,
		fmt.Sprintf("PRAGMA application_id = %d", applicationID),
		"PRAGMA user_version = 99",
		"CREATE TABLE future_things (x INTEGER)",
	)

	_, err := Open(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "supports at most") {
		t.Fatalf("Open = %v, want the unsupported-schema refusal", err)
	}
	version, journalMode := rawState(t, dir)
	if version != 99 {
		t.Errorf("user_version after refusal = %d, want 99 untouched", version)
	}
	if !strings.EqualFold(journalMode, "delete") {
		t.Errorf("journal_mode after refusal = %q; the refused database was converted", journalMode)
	}
}

// TestOpenRefusesForeignVersionZeroDatabase pins the ownership rule: an
// existing database with content but SQLite's default user_version 0 is some
// other application's file, not a blank history — it is refused with its
// content, version and journal mode untouched, never adopted by running the
// initial migration into it.
func TestOpenRefusesForeignVersionZeroDatabase(t *testing.T) {
	dir := t.TempDir()
	seedDatabase(t, dir,
		"CREATE TABLE notes (body TEXT)",
		"INSERT INTO notes (body) VALUES ('keep me')",
	)

	_, err := Open(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "did not create") {
		t.Fatalf("Open = %v, want the foreign-database refusal", err)
	}

	inspect, err := openBare(dir)
	if err != nil {
		t.Fatalf("opening bare connection: %v", err)
	}
	defer inspect.Close()
	var body string
	if err := inspect.QueryRow("SELECT body FROM notes").Scan(&body); err != nil || body != "keep me" {
		t.Errorf("foreign content = %q, %v; want it untouched", body, err)
	}
	var historyTables int
	if err := inspect.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('matches', 'match_players')").
		Scan(&historyTables); err != nil {
		t.Fatalf("counting tables: %v", err)
	}
	if historyTables != 0 {
		t.Errorf("%d history tables were created inside the foreign database", historyTables)
	}
	inspect.Close()
	version, journalMode := rawState(t, dir)
	if version != 0 || !strings.EqualFold(journalMode, "delete") {
		t.Errorf("foreign database left at version %d, journal_mode %q; want 0 and delete", version, journalMode)
	}
}

// TestOpenRefusesNewerSchemaWithHotJournalUntouched pins the strongest form
// of the refusal guarantee: a newer-schema database frozen mid-transaction —
// database plus hot rollback journal, the on-disk shape of a crash — is
// refused byte-for-byte untouched. A read-write inspection would already
// have replayed and deleted that journal just by reading.
func TestOpenRefusesNewerSchemaWithHotJournalUntouched(t *testing.T) {
	ctx := context.Background()
	seedDir := t.TempDir()
	seedDatabase(t, seedDir,
		fmt.Sprintf("PRAGMA application_id = %d", applicationID),
		"PRAGMA user_version = 99",
		"CREATE TABLE future_things (x INTEGER)",
		"INSERT INTO future_things (x) VALUES (1)",
	)

	// Freeze the mid-transaction state by copying both files while a write
	// transaction holds its rollback journal open.
	seed, err := openBare(seedDir)
	if err != nil {
		t.Fatalf("reopening seed database: %v", err)
	}
	defer seed.Close()
	tx, err := seed.Begin()
	if err != nil {
		t.Fatalf("beginning journal-holding transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO future_things (x) VALUES (2)"); err != nil {
		t.Fatalf("dirtying the transaction: %v", err)
	}
	dbBytes, err := os.ReadFile(filepath.Join(seedDir, dbFileName))
	if err != nil {
		t.Fatalf("copying database: %v", err)
	}
	journalBytes, err := os.ReadFile(filepath.Join(seedDir, dbFileName+"-journal"))
	if err != nil {
		t.Fatalf("copying hot journal: %v", err)
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, dbFileName)
	journalPath := filepath.Join(dir, dbFileName+"-journal")
	if err := os.WriteFile(dbPath, dbBytes, 0o600); err != nil {
		t.Fatalf("planting database: %v", err)
	}
	if err := os.WriteFile(journalPath, journalBytes, 0o600); err != nil {
		t.Fatalf("planting journal: %v", err)
	}

	_, err = Open(ctx, dir)
	if err == nil || !strings.Contains(err.Error(), "supports at most") {
		t.Fatalf("Open = %v, want the unsupported-schema refusal", err)
	}

	dbAfter, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("re-reading database: %v", err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("re-reading journal: %v; the refused database's journal was removed", err)
	}
	if string(dbAfter) != string(dbBytes) {
		t.Error("the refused database's bytes changed")
	}
	if string(journalAfter) != string(journalBytes) {
		t.Error("the refused database's hot journal changed")
	}
}

// plantUncheckpointedNewerWAL builds the state a raw header cannot judge: a
// genuine history database whose main-file header still says schema 1 while
// an uncheckpointed WAL carries version 99. It returns the planted directory,
// holding exactly the database and its WAL.
func plantUncheckpointedNewerWAL(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	seedDir := t.TempDir()
	seeded, err := Open(ctx, seedDir)
	if err != nil {
		t.Fatalf("creating the seed database: %v", err)
	}
	// The last close checkpoints and removes the WAL, leaving a plain
	// schema-1 database whose header is fully caught up.
	if err := seeded.Close(); err != nil {
		t.Fatalf("closing the seed database: %v", err)
	}

	seed, err := openBare(seedDir)
	if err != nil {
		t.Fatalf("reopening seed database: %v", err)
	}
	defer seed.Close()
	if _, err := seed.Exec("PRAGMA wal_autocheckpoint = 0"); err != nil {
		t.Fatalf("disabling autocheckpoint: %v", err)
	}
	if _, err := seed.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("committing the newer version into the WAL: %v", err)
	}
	// Freeze the uncheckpointed state by copying the database and WAL while
	// the connection is still open — its close would checkpoint them.
	dbBytes, err := os.ReadFile(filepath.Join(seedDir, dbFileName))
	if err != nil {
		t.Fatalf("copying database: %v", err)
	}
	walBytes, err := os.ReadFile(filepath.Join(seedDir, dbFileName+"-wal"))
	if err != nil {
		t.Fatalf("copying WAL: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dbFileName), dbBytes, 0o600); err != nil {
		t.Fatalf("planting database: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, dbFileName+"-wal"), walBytes, 0o600); err != nil {
		t.Fatalf("planting WAL: %v", err)
	}
	header, err := readHeader(filepath.Join(dir, dbFileName))
	if err != nil || header.version != 1 {
		t.Fatalf("planted header version = %d, %v; want 1 so the header alone cannot decide this database", header.version, err)
	}
	return dir
}

// TestOpenRefusesNewerVersionInUncheckpointedWAL pins the refusal path the
// raw header cannot settle: the header says schema 1 while the WAL carries
// version 99. The refusal must come from the private-copy snapshot, and the
// directory must keep exactly the two planted files byte-identical — in
// particular no shared-memory file may appear, which even a read-only SQLite
// connection to the real file would create.
func TestOpenRefusesNewerVersionInUncheckpointedWAL(t *testing.T) {
	dir := plantUncheckpointedNewerWAL(t)
	before := directoryState(t, dir)

	_, err := Open(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "supports at most") {
		t.Fatalf("Open = %v, want the unsupported-schema refusal", err)
	}
	requireDirectoryUnchanged(t, dir, before)
}

// TestOpenRefusesBlankDatabaseWithSurvivingWAL pins the stranded-sidecar
// guard: a zero-length or altogether missing database file next to a genuine
// WAL — the on-disk shape after an interrupted or damaged database, where
// the WAL may hold the only surviving copy of its contents. Initializing a
// fresh schema there would destroy that WAL, so Open must refuse and the
// directory must stay byte-identical; in the missing-file case not even an
// empty database file may appear.
func TestOpenRefusesBlankDatabaseWithSurvivingWAL(t *testing.T) {
	blankers := map[string]func(*testing.T, string){
		"zero-length": func(t *testing.T, path string) {
			if err := os.Truncate(path, 0); err != nil {
				t.Fatalf("truncating the database file: %v", err)
			}
		},
		"absent": func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatalf("removing the database file: %v", err)
			}
		},
	}
	for name, blank := range blankers {
		t.Run(name, func(t *testing.T) {
			dir := plantUncheckpointedNewerWAL(t)
			blank(t, filepath.Join(dir, dbFileName))
			before := directoryState(t, dir)

			_, err := Open(context.Background(), dir)
			if err == nil || !strings.Contains(err.Error(), "refusing to initialize a new database over it") {
				t.Fatalf("Open = %v, want the stranded-sidecar refusal", err)
			}
			requireDirectoryUnchanged(t, dir, before)
		})
	}
}

// TestOpenRefusesWhenSnapshotCannotBeTaken pins that a failed inspection
// snapshot refuses the open instead of falling back to the real database. The
// planted state makes the stakes concrete: the main header says schema 1 —
// which must not authorize anything, being legitimately stale in WAL mode —
// while the uncheckpointed WAL carries version 99, so opening the real pool
// "because the header looks fine" would replay and checkpoint a database this
// build has to refuse. With the temporary directory unusable, Open must fail
// and the source directory must stay byte-identical, WAL included.
func TestOpenRefusesWhenSnapshotCannotBeTaken(t *testing.T) {
	dir := plantUncheckpointedNewerWAL(t)
	before := directoryState(t, dir)

	// Break snapshot creation: point every temporary-directory variable the
	// platforms consult at a directory that does not exist.
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)

	_, err := Open(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "refusing to open it uninspected") {
		t.Fatalf("Open = %v, want the uninspected-open refusal", err)
	}
	requireDirectoryUnchanged(t, dir, before)
}

// TestOpenRefusesForeignHotJournalUntouched pins snapshot-phase nonmutation
// with a hot rollback journal: a foreign version-zero database — which only
// the private-copy snapshot can refuse, since its raw header looks like a
// blank history's — frozen mid-transaction is refused with the database and
// its hot journal byte-identical and no file added or removed. The journal
// replay that refusal needed happened on the discarded copy.
func TestOpenRefusesForeignHotJournalUntouched(t *testing.T) {
	ctx := context.Background()
	seedDir := t.TempDir()
	seedDatabase(t, seedDir,
		"CREATE TABLE notes (body TEXT)",
		"INSERT INTO notes (body) VALUES ('keep me')",
	)
	seed, err := openBare(seedDir)
	if err != nil {
		t.Fatalf("reopening seed database: %v", err)
	}
	defer seed.Close()
	tx, err := seed.Begin()
	if err != nil {
		t.Fatalf("beginning journal-holding transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO notes (body) VALUES ('mid-flight')"); err != nil {
		t.Fatalf("dirtying the transaction: %v", err)
	}
	dbBytes, err := os.ReadFile(filepath.Join(seedDir, dbFileName))
	if err != nil {
		t.Fatalf("copying database: %v", err)
	}
	journalBytes, err := os.ReadFile(filepath.Join(seedDir, dbFileName+"-journal"))
	if err != nil {
		t.Fatalf("copying hot journal: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dbFileName), dbBytes, 0o600); err != nil {
		t.Fatalf("planting database: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, dbFileName+"-journal"), journalBytes, 0o600); err != nil {
		t.Fatalf("planting journal: %v", err)
	}
	header, err := readHeader(filepath.Join(dir, dbFileName))
	if err != nil || header.blank || header.version != 0 || header.appID != 0 {
		t.Fatalf("planted header = %+v, %v; want a nonblank version-0 unstamped header so the refusal must come from the snapshot", header, err)
	}
	before := directoryState(t, dir)

	_, err = Open(ctx, dir)
	if err == nil || !strings.Contains(err.Error(), "did not create") {
		t.Fatalf("Open = %v, want the foreign-database refusal", err)
	}
	requireDirectoryUnchanged(t, dir, before)
}

// TestOpenRefusesNegativeSchemaVersion pins that a malformed negative
// user_version is a clean refusal — through Open and through migrate driven
// directly — never an out-of-range index into the migration table.
func TestOpenRefusesNegativeSchemaVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedDatabase(t, dir,
		fmt.Sprintf("PRAGMA application_id = %d", applicationID),
		"PRAGMA user_version = -1",
	)

	if _, err := Open(ctx, dir); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("Open = %v, want the malformed-version refusal", err)
	}

	pool, err := sql.Open("sqlite", databaseDSN(filepath.Join(dir, dbFileName), poolQuery))
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	defer pool.Close()
	db := &DB{sql: pool, path: filepath.Join(dir, dbFileName)}
	if err := db.migrate(ctx); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("migrate = %v, want the malformed-version refusal", err)
	}
}

// TestOpenResolvesRelativeDirectories pins that a relative directory
// override targets the working directory's subdirectory, not an absolute
// path invented by the DSN builder.
func TestOpenResolvesRelativeDirectories(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)

	db := openTestDBAt(t, filepath.Join("relative", "history"))
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{})

	if _, err := os.Stat(filepath.Join(base, "relative", "history", dbFileName)); err != nil {
		t.Fatalf("database is not under the working directory: %v", err)
	}
	matches, err := db.ListMatches(context.Background())
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %d, %v; want the stored 1", len(matches), err)
	}
}

// TestOpenTightensPreexistingFileMode pins the privacy guarantee for a file
// someone pre-created with a wider mode: acceptance tightens it to
// owner-only, since OpenFile's mode applies only to files it creates.
func TestOpenTightensPreexistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, dbFileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("pre-creating wide-mode file: %v", err)
	}

	db := openTestDBAt(t, dir)
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode = %o after acceptance, want 600", perm)
	}
}

// TestOpenRefusesForeignApplicationID pins that a versioned database stamped
// by another application is refused even when its version looks compatible.
func TestOpenRefusesForeignApplicationID(t *testing.T) {
	dir := t.TempDir()
	seedDatabase(t, dir,
		"PRAGMA application_id = 305419896", // 0x12345678, some other tool
		"PRAGMA user_version = 1",
	)

	_, err := Open(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "different application") {
		t.Fatalf("Open = %v, want the foreign-application refusal", err)
	}
}

// TestOpenAcceptsSpecialCharacterDirectories pins DSN escaping: a directory
// whose name contains ?, #, % and spaces must hold the real database instead
// of the path being torn apart at the first '?' into a truncated filename
// plus bogus connection options.
func TestOpenAcceptsSpecialCharacterDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("? and # are not legal in Windows file names")
	}
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "odd? dir#100% full")
	db := openTestDBAt(t, dir)
	storeFixtureMatch(t, db, testDigest(1), fixtureOptions{})

	if _, err := os.Stat(filepath.Join(dir, dbFileName)); err != nil {
		t.Fatalf("database is not inside the literal directory: %v", err)
	}
	matches, err := db.ListMatches(ctx)
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %d, %v; want the stored 1", len(matches), err)
	}
}

// TestConcurrentOpenInitializesOnce races several processes-worth of Open
// calls on one fresh directory: every one must succeed, because a migration
// re-checks the schema version once it holds the write lock instead of
// colliding on tables a concurrent initializer already created.
func TestConcurrentOpenInitializesOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const openers = 4
	errs := make([]error, openers)
	var wg sync.WaitGroup
	for i := range openers {
		wg.Go(func() {
			db, err := Open(ctx, dir)
			if err == nil {
				db.Close()
			}
			errs[i] = err
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Open %d: %v", i, err)
		}
	}
	db := openTestDBAt(t, dir)
	if version, err := db.userVersion(ctx); err != nil || version != 1 {
		t.Fatalf("user_version = %d, %v; want 1", version, err)
	}
}

// TestOpenRefusesCorruptDatabase pins that a file that is not a SQLite
// database is refused with its bytes intact — never silently deleted or
// recreated.
func TestOpenRefusesCorruptDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, dbFileName)
	garbage := []byte("this is not a sqlite database, just sixty-some bytes of text!!")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatalf("writing garbage: %v", err)
	}

	_, err := Open(context.Background(), dir)
	if err == nil {
		t.Fatal("Open accepted a corrupt database")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the database path", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("re-reading the file: %v", readErr)
	}
	if string(after) != string(garbage) {
		t.Error("the corrupt file was modified by the refused open")
	}
}

// TestOpenRefusesClaimedSchemaWithoutTables pins that a database claiming
// this tool's identity and current schema version but lacking the tables is
// refused rather than repaired — and refused before the pool, so the
// directory keeps its exact bytes and the database is not converted to WAL
// on the way to the refusal.
func TestOpenRefusesClaimedSchemaWithoutTables(t *testing.T) {
	dir := t.TempDir()
	seedDatabase(t, dir,
		fmt.Sprintf("PRAGMA application_id = %d", applicationID),
		"PRAGMA user_version = 1",
	)
	before := directoryState(t, dir)

	_, err := Open(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "does not understand") {
		t.Fatalf("Open = %v, want the unknown-schema refusal", err)
	}
	requireDirectoryUnchanged(t, dir, before)
	if _, journalMode := rawState(t, dir); !strings.EqualFold(journalMode, "delete") {
		t.Errorf("journal_mode after refusal = %q; the refused database was converted", journalMode)
	}
}

// TestMigrationRollback forces the initial migration to fail partway — a
// conflicting table already occupies a name it creates — and requires the
// whole migration to roll back: user_version stays 0 and no other schema-1
// table exists. It drives migrate directly because Open's foreign-database
// inspection would refuse such a file before any migration ran.
func TestMigrationRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedDatabase(t, dir, "CREATE TABLE match_players (filler INTEGER)")

	pool, err := sql.Open("sqlite", databaseDSN(filepath.Join(dir, dbFileName), poolQuery))
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	defer pool.Close()
	db := &DB{sql: pool, path: filepath.Join(dir, dbFileName)}

	err = db.migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "migrating history schema from version 0 to 1") {
		t.Fatalf("migrate = %v, want a migration failure", err)
	}

	var version int
	if err := pool.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("reading user_version: %v", err)
	}
	if version != 0 {
		t.Errorf("user_version = %d after failed migration, want 0", version)
	}
	var count int
	if err := pool.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('matches', 'display_selections', 'display_preferences')").
		Scan(&count); err != nil {
		t.Fatalf("counting tables: %v", err)
	}
	if count != 0 {
		t.Errorf("%d schema-1 tables survived the rolled-back migration, want 0", count)
	}
}

// TestForeignKeysEnforced pins both directions of referential integrity: a
// child row without its parent is rejected, and deleting a match cascades
// through its players to their display preferences.
func TestForeignKeysEnforced(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO match_players (match_sha256, steam_id, alias, player_json, facts_json)
		VALUES (?, ?, ?, ?, ?)`,
		testDigest(1), "76561198000000001", "orphan", []byte("{}"), []byte("{}"))
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("orphan player insert = %v, want a foreign key failure", err)
	}

	stored := storeFixtureMatch(t, db, testDigest(1), fixtureOptions{
		selected: []uint64{fixturePlayerOne},
	})
	if _, err := db.sql.ExecContext(ctx,
		"DELETE FROM matches WHERE demo_sha256 = ?", stored); err != nil {
		t.Fatalf("deleting match: %v", err)
	}
	for _, table := range []string{"match_players", "display_preferences"} {
		var count int
		if err := db.sql.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still has %d rows after the cascade delete, want 0", table, count)
		}
	}
}

// TestConcurrentAccessWaitsOutTheWriteLock holds a write transaction on one
// handle while a second handle stores a match: the busy timeout must let the
// second writer queue and succeed instead of failing with a busy error.
func TestConcurrentAccessWaitsOutTheWriteLock(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first := openTestDBAt(t, dir)
	second := openTestDBAt(t, dir)

	tx, err := first.sql.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginning blocking transaction: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM matches"); err != nil {
		t.Fatalf("taking the write lock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := second.StoreMatch(ctx, fixtureStoreInput(testDigest(2), fixtureOptions{}))
		done <- err
	}()

	// Hold the lock briefly — well inside the 5000ms busy timeout — then
	// release it and require the queued writer to finish.
	time.Sleep(250 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("second writer finished with %v while the lock was held", err)
	default:
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("releasing the lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("queued store failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("queued store never finished after the lock was released")
	}
}

// TestOpenKeepsDatabaseOwnerPrivate pins the on-disk permissions where the
// platform supports POSIX modes: the directory shields the WAL and
// shared-memory files, and the database file itself is owner-only.
func TestOpenKeepsDatabaseOwnerPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "nested", "history")
	openTestDBAt(t, dir)

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("history directory mode = %o, want 700", perm)
	}
	fileInfo, err := os.Stat(filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode = %o, want 600", perm)
	}
}
