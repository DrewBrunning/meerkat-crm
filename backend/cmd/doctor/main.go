// Command doctor is "mycorrhizal doctor" (DB-01, issue #460): the
// operator-runnable data-integrity checker. It runs the two SQLite structural
// pragmas (PRAGMA integrity_check + foreign_key_check) and the per-invariant
// application checks from docs/adrs/0012-canonical-database-invariants.md
// against a database file, and reports what it finds — relationships pointing
// at deleted contacts, orphaned join rows, dangling external references,
// malformed canonical records, derived-index divergence.
//
// After a restore, a migration, or a bulk import is exactly when an operator
// wants to ask, which is why this is a CLI and not only the scheduled job or
// the GET /admin/integrity-check endpoint.
//
// Detection is the default and is read-only. Repair is opt-in and separate:
//
//	doctor                       # detect; exit 1 if any violation, 0 if clean
//	doctor -repair               # dry run: print what repair WOULD delete
//	doctor -repair -confirm      # actually delete truly-orphaned join/edge rows
//	doctor -json                 # machine-readable output (any mode)
//	doctor -db /path/to/app.db   # or set SQLITE_DB_PATH
//
// Repair only ever removes hard-delete join/edge rows whose referenced parent
// does not exist at all (never a merely soft-deleted one) — per ADR 0004 these
// are bounded, re-derivable rows, so a genuinely orphaned one carries no
// recoverable data. Repair refuses to run at all unless the storage PRAGMA pass
// is OK (issue #921): deleting from a structurally corrupt database is unsafe,
// and the refusal gets its own exit code (3) so automation can tell it from
// findings. The database must already be at the current schema (run the server
// or cmd/migrate first); doctor does not migrate.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"mycorrhizal/config"
	"mycorrhizal/database"
	"mycorrhizal/services"

	"gorm.io/gorm"
)

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr)) // # pragma: no cover — os.Exit terminates; tests call runCLI directly
}

// doctorResult is the -json payload for a detection run.
type doctorResult struct {
	OK      bool                            `json:"ok"`
	Storage services.StorageIntegrityReport `json:"storage"`
	Data    services.DataIntegrityReport    `json:"data"`
}

// repairRefusal is the -json payload when -repair is declined by the storage
// integrity gate (issue #921). Deliberately a different shape from
// services.RepairReport so a caller can never mistake a refusal for a completed
// repair (which would otherwise marshal as an empty/zero report).
type repairRefusal struct {
	OK      bool   `json:"ok"`
	Refused bool   `json:"refused"`
	Reason  string `json:"reason"`
}

// runCLI parses args, opens the database, and runs the requested mode. Exit
// codes: 0 = clean (or repair completed / dry-run printed), 1 = violations
// found or a pass could not complete, 2 = usage or open error, 3 = repair
// refused because the storage integrity pass was not OK (issue #921).
func runCLI(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "path to the SQLite database file (default: $SQLITE_DB_PATH, then mycorrhizal.db)")
	repair := fs.Bool("repair", false, "repair truly-orphaned hard-delete join/edge rows (dry run unless -confirm)")
	confirm := fs.Bool("confirm", false, "with -repair: actually perform the deletions")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "doctor: unexpected argument(s); use -db to pass the database path")
		return 2
	}
	if *confirm && !*repair {
		fmt.Fprintln(stderr, "doctor: -confirm has no effect without -repair")
		return 2
	}

	path := resolveDBPath(*dbPath)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("no such file")
		}
		fmt.Fprintf(stderr, "doctor: open %q: %v\n", path, err)
		return 2
	}

	db, err := database.OpenMigratedFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "doctor: open %q: %v\n", path, err)
		return 2
	}
	defer closeDB(db)

	cfg := config.LoadConfig()

	if *repair {
		return runRepair(context.Background(), db, *confirm, *asJSON, stdout, stderr)
	}
	return runDetect(context.Background(), db, *cfg, *asJSON, stdout, stderr)
}

// resolveDBPath mirrors the other CLIs' fallback order: -db flag, then
// SQLITE_DB_PATH, then the default filename.
func resolveDBPath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if p := os.Getenv("SQLITE_DB_PATH"); p != "" {
		return p
	}
	return "mycorrhizal.db"
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func runDetect(ctx context.Context, db *gorm.DB, cfg config.Config, asJSON bool, stdout, stderr io.Writer) int {
	storage, sErr := services.RunStorageIntegrityChecks(db)
	data, dErr := services.RunDataIntegrityChecks(ctx, db, cfg)

	res := doctorResult{
		Storage: storage,
		Data:    data,
		OK:      sErr == nil && dErr == nil && storage.OK && data.OK,
	}

	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		printDetect(stdout, res, sErr, dErr)
	}

	if !res.OK {
		return 1
	}
	return 0
}

func printDetect(w io.Writer, res doctorResult, sErr, dErr error) {
	fmt.Fprintln(w, "== storage pass (SQLite pragmas) ==")
	if sErr != nil {
		fmt.Fprintf(w, "  ERROR: %v\n", sErr)
	} else if res.Storage.OK {
		fmt.Fprintln(w, "  ok: integrity_check + foreign_key_check clean")
	} else {
		fmt.Fprintf(w, "  FAIL: %s\n", res.Storage.Detail())
	}

	fmt.Fprintln(w, "== data pass (ADR 0012 invariants) ==")
	if dErr != nil {
		fmt.Fprintf(w, "  ERROR: a probe could not complete: %v\n", dErr)
	}
	if len(res.Data.Findings) == 0 {
		fmt.Fprintln(w, "  ok: no findings")
	}
	for _, f := range res.Data.Findings {
		scope := "instance-wide"
		if f.UserID != 0 {
			scope = fmt.Sprintf("user %d", f.UserID)
		}
		rep := ""
		if f.Repairable {
			rep = " [repairable]"
		}
		fmt.Fprintf(w, "  %-9s %-8s %-40s %s (%s)%s\n",
			f.Severity, f.Invariant, f.Check, f.Detail, scope, rep)
	}

	if res.OK {
		fmt.Fprintln(w, "\nresult: OK")
	} else {
		fmt.Fprintln(w, "\nresult: PROBLEMS FOUND — see above; `doctor -repair` previews the safe fixes")
	}
}

func runRepair(ctx context.Context, db *gorm.DB, confirm, asJSON bool, stdout, stderr io.Writer) int {
	// Storage-integrity gate (issue #921): repair issues destructive
	// `DELETE ... WHERE NOT EXISTS` against every repairable table. If the pages
	// are already structurally unsound, those scans can read garbage, so a
	// "repair" on a corrupt database risks destroying rows it never correctly
	// read. Refuse unless the storage pass is clean, with a distinct exit code
	// (3) so a script can tell "refused" from "repaired" and from "findings".
	storage, sErr := services.RunStorageIntegrityChecks(db)
	switch {
	case sErr != nil:
		msg := fmt.Sprintf("storage integrity check could not run: %v", sErr)
		emitRepairRefusal(asJSON, stdout, msg)
		fmt.Fprintf(stderr, "doctor: refusing to repair: %s\n", msg)
		return 3
	case !storage.OK:
		detail := storage.Detail()
		msg := fmt.Sprintf("storage integrity is not OK (%s)", detail)
		emitRepairRefusal(asJSON, stdout, msg)
		fmt.Fprintf(stderr, "doctor: refusing to repair: %s — restore from a good backup before repairing\n", msg)
		return 3
	}

	report, err := services.RepairDataIntegrity(ctx, db, services.RepairOptions{DryRun: !confirm})
	if err != nil {
		fmt.Fprintf(stderr, "doctor: repair failed: %v\n", err)
		return 1
	}

	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return 0
	}

	if report.DryRun {
		fmt.Fprintln(stdout, "== repair DRY RUN (no changes made) ==")
	} else {
		fmt.Fprintln(stdout, "== repair ==")
	}
	if len(report.Actions) == 0 {
		fmt.Fprintln(stdout, "  nothing to repair")
		return 0
	}
	for _, a := range report.Actions {
		fmt.Fprintf(stdout, "  %-40s %s\n", a.Check, a.Detail)
	}
	fmt.Fprintf(stdout, "\n%d row(s) %s\n", report.TotalRows(),
		map[bool]string{true: "would be deleted — re-run with -confirm to apply", false: "deleted"}[report.DryRun])
	return 0
}

// emitRepairRefusal writes the machine-readable refusal payload when -json is
// set. It augments, never replaces, the human stderr line the caller prints
// (the same stderr channel every other doctor error uses).
func emitRepairRefusal(asJSON bool, stdout io.Writer, reason string) {
	if !asJSON {
		return
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(repairRefusal{OK: false, Refused: true, Reason: reason})
}
