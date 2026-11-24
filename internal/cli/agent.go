package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/ident"
)

// archiveAgent archives the Paseo agent id on this machine (paseo archive),
// interrupting it if it is running. Only an agent this machine's daemon has
// a record of is archived, so the dashboard cannot pass anything else to
// paseo. Sending it a message from the desk later revives it.
func (a *App) archiveAgent(id string) (string, error) {
	id = strings.TrimSpace(id)
	rec, ok := ident.PaseoRecord(a.paseoHome(), id)
	if !ok || rec.ID != id {
		return "", fmt.Errorf("%s is not a Paseo agent on this machine", id)
	}
	name := rec.Title
	if name == "" {
		name = short8(id)
	}
	if rec.ArchivedAt != nil {
		return name + " is already archived.", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := a.runExt(ctx, nil, "paseo", "archive", id); err != nil {
		return "", fmt.Errorf("paseo archive %s: %v", id, paseoError(out))
	}
	return "Archived " + name + ".", nil
}

// apiAgentAct acts on a Paseo agent: archive.
func (s *server) apiAgentAct(app *App, body request) (any, error) {
	switch body.Action {
	case "archive":
		note, err := app.archiveAgent(body.ID)
		// Read Paseo's records again: the page reloads after this, and its
		// views and the Out of quota list then leave the agent out.
		expireLive()
		return map[string]string{"note": note}, err
	}
	return nil, usagef("agent action is archive")
}
