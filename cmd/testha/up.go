package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

func up(ctx context.Context, dir string) error {
	// configuration.yaml includes these; a fresh start has none yet.
	for name, empty := range map[string]string{"automations.yaml": "[]\n", "scenes.yaml": "[]\n", "scripts.yaml": "{}\n"} {
		path := filepath.Join(dir, "ha", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(empty), 0o644); err != nil {
				return err
			}
		}
	}
	if err := compose(ctx, dir, "up", "-d"); err != nil {
		return err
	}
	if err := waitForHA(ctx); err != nil {
		return err
	}
	e := loadEnv(dir)
	if e["TESTHA_ADMIN_PASSWORD"] == "" {
		e["TESTHA_ADMIN_PASSWORD"] = randomHex(8)
	}
	if e["MIRA_API_TOKEN"] == "" {
		e["MIRA_API_TOKEN"] = randomHex(16)
	}
	e["HA_HOST"] = strings.TrimPrefix(haURL, "http://")

	if !tokenWorks(ctx, e["TESTHA_ADMIN_TOKEN"]) {
		session, err := onboard(ctx, e["TESTHA_ADMIN_PASSWORD"])
		if err != nil {
			return fmt.Errorf("onboarding: %w", err)
		}
		c, stop, err := connect(ctx, session)
		if err != nil {
			return err
		}
		e["TESTHA_ADMIN_TOKEN"], err = longLivedToken(ctx, c, "testha")
		stop()
		if err != nil {
			return err
		}
	}
	if err := e.save(dir); err != nil {
		return err
	}

	admin, stop, err := connect(ctx, e["TESTHA_ADMIN_TOKEN"])
	if err != nil {
		return err
	}
	defer stop()
	if !tokenWorks(ctx, e["HA_TOKEN"]) {
		if e["TESTHA_MIRA_PASSWORD"] == "" {
			e["TESTHA_MIRA_PASSWORD"] = randomHex(8)
		}
		if e["HA_TOKEN"], err = miraToken(ctx, admin, e["TESTHA_MIRA_PASSWORD"]); err != nil {
			return fmt.Errorf("mira user: %w", err)
		}
		if err := e.save(dir); err != nil {
			return err
		}
	}
	if err := seed(ctx, admin, e["TESTHA_ADMIN_TOKEN"], parseExtras(e["TESTHA_EXPOSE"])); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	fmt.Printf("test HA ready at %s (admin / %s); tokens in %s/.env\n", haURL, e["TESTHA_ADMIN_PASSWORD"], dir)
	return nil
}

// miraToken makes sure the non-admin user "mira" exists and returns a new
// long-lived token for it.
func miraToken(ctx context.Context, admin *haws.Client, password string) (string, error) {
	var users []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := call(ctx, admin, map[string]any{"type": "config/auth/list"}, &users); err != nil {
		return "", err
	}
	userID := ""
	for _, u := range users {
		if u.Username == "mira" {
			userID = u.ID
		}
	}
	if userID == "" {
		var created struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		if err := call(ctx, admin, map[string]any{
			"type": "config/auth/create", "name": "Mira", "group_ids": []string{"system-users"},
		}, &created); err != nil {
			return "", err
		}
		userID = created.User.ID
		if err := call(ctx, admin, map[string]any{
			"type": "config/auth_provider/homeassistant/create", "user_id": userID, "username": "mira", "password": password,
		}, nil); err != nil {
			return "", err
		}
	} else if err := call(ctx, admin, map[string]any{
		"type": "config/auth_provider/homeassistant/admin_change_password", "user_id": userID, "password": password,
	}, nil); err != nil {
		return "", err
	}
	session, err := login(ctx, "mira", password)
	if err != nil {
		return "", err
	}
	c, stop, err := connect(ctx, session)
	if err != nil {
		return "", err
	}
	defer stop()
	return longLivedToken(ctx, c, "mira "+time.Now().Format("2006-01-02 15:04"))
}

func set(ctx context.Context, dir, entityID, state string) error {
	e := loadEnv(dir)
	token := e["TESTHA_ADMIN_TOKEN"]
	if !tokenWorks(ctx, token) {
		return fmt.Errorf("no working admin token, run `testha up` first")
	}
	if sim, ok := isSimulated(entityID); ok {
		service := map[string]string{"on": "turn_on", "off": "turn_off"}[state]
		if service == "" {
			return fmt.Errorf("%s is simulated: state must be on or off", entityID)
		}
		return api(ctx, "POST", "/api/services/input_boolean/"+service, token, map[string]any{"entity_id": sim}, nil)
	}
	// Anything else: overwrite the state machine directly. The integration
	// behind the entity may overwrite it again on its next update.
	return api(ctx, "POST", "/api/states/"+entityID, token, map[string]any{"state": state}, nil)
}
