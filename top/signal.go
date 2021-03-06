package top

import (
	"fmt"
	"github.com/jroimartin/gocui"
	"github.com/lesovsky/pgcenter/internal/postgres"
	"github.com/lesovsky/pgcenter/internal/query"
	"strconv"
)

const (
	groupActive int = 1 << iota
	groupIdle
	groupIdleXact
	groupWaiting
	groupOthers
)

// killSingle sends cancel or terminate signal to a single Postgres backend.
func killSingle(db *postgres.DB, mode string, answer string) string {
	if mode != "cancel" && mode != "terminate" {
		return "Signals: do nothing, unknown mode"
	}

	var q string

	switch mode {
	case "cancel":
		q = query.ExecCancelQuery
	case "terminate":
		q = query.ExecTerminateBackend
	}

	pid, err := strconv.Atoi(answer)
	if err != nil {
		return fmt.Sprintf("Signals: do nothing, %s", err.Error())
	}

	_, err = db.Exec(q, pid)
	if err != nil {
		return fmt.Sprintf("Signals: do nothing, %s", err.Error())
	}

	return "Signals: done"
}

// killGroup sends cancel or terminate signal to group of Postgres backends.
func killGroup(app *app, mode string) string {
	if app.config.view.Name != "activity" {
		return "Signals: sending signals allowed in pg_stat_activity only"
	}

	mask := app.config.procMask

	if mask == 0 {
		return "Signals: do nothing, process mask is empty"
	}

	if mode != "cancel" && mode != "terminate" {
		return "Signals: do nothing, unknown mode"
	}

	// Select signal function: pg_cancel_backend or pg_terminate_backend.
	var template string
	switch mode {
	case "cancel":
		template = query.ExecCancelQueryGroup
	case "terminate":
		template = query.ExecTerminateBackendGroup
	}

	// states defines SQL expression conditions necessary for selecting group of target processes.
	var states = map[int]string{
		groupIdle:     "state = 'idle'",
		groupIdleXact: "state IN ('idle in transaction (aborted)', 'idle in transaction')",
		groupActive:   "state = 'active'",
		groupWaiting:  "wait_event_type = 'Lock'",
		groupOthers:   "state IN ('fastpath function call', 'disabled')",
	}

	// Walk through the states, if state is in the mask then send signal to that group of process.
	var signalled, signalledTotal int64
	for state, part := range states {
		if (mask & state) != 0 {
			app.config.queryOptions.BackendState = part
			if state == groupWaiting && app.postgresProps.VersionNum < 90600 {
				app.config.queryOptions.BackendState = "waiting"
			}

			// format query
			q, err := query.Format(template, app.config.queryOptions)
			if err != nil {
				return fmt.Sprintf("Signals: %s", err.Error())
			}

			// execute query
			err = app.db.QueryRow(q).Scan(&signalled)
			if err != nil {
				return fmt.Sprintf("Signals: %s", err.Error())
			}

			signalledTotal += signalled
		}
	}

	var msg string
	switch mode {
	case "cancel":
		msg = "Signals: cancelled " + strconv.FormatInt(signalledTotal, 10) + " queries."
	case "terminate":
		msg = "Signals: terminated " + strconv.FormatInt(signalledTotal, 10) + " backends."
	}

	return msg
}

// setProcMask set process mask.
func setProcMask(answer string, config *config) string {
	// Reset existing mask.
	config.procMask = 0

	for _, ch := range answer {
		switch string(ch) {
		case "i":
			config.procMask |= groupIdle
		case "x":
			config.procMask |= groupIdleXact
		case "a":
			config.procMask |= groupActive
		case "w":
			config.procMask |= groupWaiting
		case "o":
			config.procMask |= groupOthers
		}
	}

	return printMaskString(config.procMask)
}

// showProcMask UI-wrapper over printMaskString.
func showProcMask(config *config) func(g *gocui.Gui, _ *gocui.View) error {
	return func(g *gocui.Gui, _ *gocui.View) error {

		printCmdline(g, printMaskString(config.procMask))
		return nil
	}
}

// printMaskString parses bitmap mask into human readable string with names.
func printMaskString(mask int) string {
	ct := "Mask: "
	if mask == 0 {
		ct += "empty "
	}
	if (mask & groupIdle) != 0 {
		ct += "idle "
	}
	if (mask & groupIdleXact) != 0 {
		ct += "idle_xact "
	}
	if (mask & groupActive) != 0 {
		ct += "active "
	}
	if (mask & groupWaiting) != 0 {
		ct += "waiting "
	}
	if (mask & groupOthers) != 0 {
		ct += "others "
	}

	return ct
}
