package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type migrationFile struct {
	base string
	up   string
	down string
}

func main() {
	var migrationsDir string
	var verbose bool
	flag.StringVar(&migrationsDir, "migrations", "../../migrations", "Path to migration SQL files")
	flag.BoolVar(&verbose, "verbose", false, "Print embedded Postgres logs")
	flag.Parse()

	ctx := context.Background()
	if err := run(ctx, migrationsDir, verbose); err != nil {
		fmt.Fprintf(os.Stderr, "migration smoke failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("migration smoke ok")
}

func run(ctx context.Context, migrationsDir string, verbose bool) error {
	migrationsDir, err := filepath.Abs(migrationsDir)
	if err != nil {
		return fmt.Errorf("resolve migrations path: %w", err)
	}
	files, err := collectMigrations(migrationsDir)
	if err != nil {
		return err
	}

	port, err := freePort()
	if err != nil {
		return err
	}
	tempRoot, err := os.MkdirTemp("", "nof0-migration-smoke-*")
	if err != nil {
		return fmt.Errorf("create temp root: %w", err)
	}
	defer os.RemoveAll(tempRoot)

	logger := io.Discard
	if verbose {
		logger = os.Stdout
	}
	pg := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).
			Port(port).
			RuntimePath(filepath.Join(tempRoot, "runtime")).
			CachePath(filepath.Join(tempRoot, "cache")).
			StartTimeout(10 * time.Minute).
			Logger(logger),
	)

	fmt.Printf("starting embedded postgres on port %d\n", port)
	if err := pg.Start(); err != nil {
		return fmt.Errorf("start embedded postgres: %w", err)
	}
	defer func() {
		if err := pg.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "stop embedded postgres: %v\n", err)
		}
	}()

	dsn := fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	if err := applyUpMigrations(ctx, db, files); err != nil {
		return err
	}
	if err := validateAfterUp(ctx, db); err != nil {
		return err
	}
	if err := applyDownMigrations(ctx, db, files); err != nil {
		return err
	}
	if err := validateAfterAllDown(ctx, db); err != nil {
		return err
	}
	return nil
}

func collectMigrations(migrationsDir string) ([]migrationFile, error) {
	upFiles, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		return nil, fmt.Errorf("glob up migrations: %w", err)
	}
	if len(upFiles) == 0 {
		return nil, fmt.Errorf("no up migrations found in %s", migrationsDir)
	}
	sort.Strings(upFiles)

	files := make([]migrationFile, 0, len(upFiles))
	for _, up := range upFiles {
		down := strings.TrimSuffix(up, ".up.sql") + ".down.sql"
		if _, err := os.Stat(down); err != nil {
			return nil, fmt.Errorf("missing down migration for %s: %w", filepath.Base(up), err)
		}
		files = append(files, migrationFile{
			base: filepath.Base(up),
			up:   up,
			down: down,
		})
	}
	return files, nil
}

func applyUpMigrations(ctx context.Context, db *sql.DB, files []migrationFile) error {
	seededConversationIDs := false
	for _, file := range files {
		fmt.Printf("apply up %s\n", file.base)
		if err := applySQL(ctx, db, file.up); err != nil {
			return err
		}
		if !seededConversationIDs {
			seeded, err := seedLegacyConversationIDs(ctx, db)
			if err != nil {
				return err
			}
			seededConversationIDs = seeded
		}
	}
	if !seededConversationIDs {
		return fmt.Errorf("conversation_messages table was never available for legacy seed")
	}
	return nil
}

func applyDownMigrations(ctx context.Context, db *sql.DB, files []migrationFile) error {
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		fmt.Printf("apply down %s\n", filepath.Base(file.down))
		if err := applySQL(ctx, db, file.down); err != nil {
			return err
		}
		if file.base == "006_db_read_model_compat.up.sql" {
			if err := validateAfter006Down(ctx, db); err != nil {
				return err
			}
		}
	}
	return nil
}

func applySQL(ctx context.Context, db *sql.DB, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if _, err := db.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("apply %s: %w", filepath.Base(path), err)
	}
	return nil
}

func seedLegacyConversationIDs(ctx context.Context, db *sql.DB) (bool, error) {
	exists, err := tableExists(ctx, db, "conversation_messages")
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO conversation_messages (trader_id, model_id, conversation_id, role, detail)
VALUES
    ($1, $2, $3, $4, $5::jsonb),
    ($6, $7, $8, $9, $10::jsonb)`,
		"trader-numeric", "model-numeric", "123", "assistant", `{}`,
		"trader-nonnumeric", "model-nonnumeric", "legacy-conversation-id", "user", `{}`,
	)
	if err != nil {
		return false, fmt.Errorf("seed legacy conversation ids: %w", err)
	}
	fmt.Println("seed legacy conversation ids")
	return true, nil
}

func validateAfterUp(ctx context.Context, db *sql.DB) error {
	if err := requireTable(ctx, db, "conversations"); err != nil {
		return err
	}
	if err := requireTable(ctx, db, "account_equity_snapshots"); err != nil {
		return err
	}
	info, err := columnInfo(ctx, db, "conversation_messages", "conversation_id")
	if err != nil {
		return err
	}
	if info.dataType != "bigint" {
		return fmt.Errorf("conversation_messages.conversation_id type = %s, want bigint", info.dataType)
	}
	nulls, err := countRows(ctx, db, `SELECT COUNT(*) FROM conversation_messages WHERE conversation_id IS NULL`)
	if err != nil {
		return err
	}
	if nulls == 0 {
		return fmt.Errorf("expected at least one NULL conversation_id after legacy nonnumeric conversion")
	}
	return nil
}

func validateAfter006Down(ctx context.Context, db *sql.DB) error {
	info, err := columnInfo(ctx, db, "conversation_messages", "conversation_id")
	if err != nil {
		return err
	}
	if info.dataType != "text" {
		return fmt.Errorf("conversation_messages.conversation_id type after 006 down = %s, want text", info.dataType)
	}
	if info.isNullable != "NO" {
		return fmt.Errorf("conversation_messages.conversation_id nullable after 006 down = %s, want NO", info.isNullable)
	}
	nulls, err := countRows(ctx, db, `SELECT COUNT(*) FROM conversation_messages WHERE conversation_id IS NULL`)
	if err != nil {
		return err
	}
	if nulls != 0 {
		return fmt.Errorf("conversation_messages has %d NULL conversation_id values after 006 down", nulls)
	}
	fallbacks, err := countRows(ctx, db, `SELECT COUNT(*) FROM conversation_messages WHERE conversation_id = id::TEXT`)
	if err != nil {
		return err
	}
	if fallbacks == 0 {
		return fmt.Errorf("expected at least one fallback conversation_id = id::TEXT after 006 down")
	}
	if err := requireNoTable(ctx, db, "conversations"); err != nil {
		return err
	}
	if err := requireNoTable(ctx, db, "account_equity_snapshots"); err != nil {
		return err
	}
	return nil
}

func validateAfterAllDown(ctx context.Context, db *sql.DB) error {
	count, err := countRows(ctx, db, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = 'public'
  AND table_type = 'BASE TABLE'`)
	if err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("expected all public base tables dropped after down migrations, found %d", count)
	}
	return nil
}

type columnMeta struct {
	dataType   string
	isNullable string
}

func columnInfo(ctx context.Context, db *sql.DB, table, column string) (columnMeta, error) {
	var info columnMeta
	err := db.QueryRowContext(ctx, `
SELECT data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = $1
  AND column_name = $2`, table, column).Scan(&info.dataType, &info.isNullable)
	if err != nil {
		return columnMeta{}, fmt.Errorf("read column info %s.%s: %w", table, column, err)
	}
	return info, nil
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var name sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1)::TEXT`, "public."+table).Scan(&name); err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return name.Valid && name.String != "", nil
}

func requireTable(ctx context.Context, db *sql.DB, table string) error {
	exists, err := tableExists(ctx, db, table)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("expected table %s to exist", table)
	}
	return nil
}

func requireNoTable(ctx context.Context, db *sql.DB, table string) error {
	exists, err := tableExists(ctx, db, table)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("expected table %s to be dropped", table)
	}
	return nil
}

func countRows(ctx context.Context, db *sql.DB, query string) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count rows: %w", err)
	}
	return count, nil
}

func freePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate port: %w", err)
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener addr %T", listener.Addr())
	}
	return uint32(addr.Port), nil
}
