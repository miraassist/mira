package main

import (
	"context"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/ui"
)

// redirectURI is Home Assistant's fixed redirect for its OAuth helper, the
// address a service is told (docs/spec/oauth.md, The login).
const redirectURI = "https://my.home-assistant.io/redirect/oauth"

// connections are the Connections page's side in the service.
type connections struct{ a *app }

func (c connections) List(ctx context.Context) []ui.Connection {
	m := c.a.oauth
	out := []ui.Connection{}
	if m == nil {
		return out
	}
	for _, name := range m.Skills() {
		id, set := m.Client(name)
		out = append(out, ui.Connection{Skill: name, Name: m.OAuth(name).Name, State: m.State(name),
			ClientID: id, SecretSet: set, Redirect: redirectURI})
	}
	return out
}

// SaveClient writes client id and secret into the skill's entry of
// mira.yaml, the secret into the secrets file, and hands them to the
// connections at once: no restart.
func (c connections) SaveClient(ctx context.Context, skill, id, secret string) error {
	m, e := c.a.oauth, c.a.edit
	if m == nil || m.OAuth(skill) == nil {
		return oauth.ErrUnknown
	}
	if e == nil {
		return errors.New("Die mira.yaml lässt sich hier nicht ändern.")
	}
	ref := config.SecretName("SKILL", skill, skills.SettingClientSecret)
	secrets := map[string]string{}
	if secret != "" {
		secrets[ref] = secret
	}
	err := e.write(secrets, func(top *yaml.Node) error {
		entries := config.MapChild(top, "skills")
		values := config.MapGet(entries, skill)
		if values == nil || values.Kind != yaml.MappingNode {
			values = &yaml.Node{Kind: yaml.MappingNode}
		}
		if id == "" {
			config.MapDelete(values, skills.SettingClientID)
		} else {
			config.MapSet(values, skills.SettingClientID, config.Scalar(id))
		}
		if secret != "" {
			config.MapSet(values, skills.SettingClientSecret, config.Scalar("${"+ref+"}"))
		}
		config.MapSet(entries, skill, values)
		return nil
	}, false)
	if err != nil {
		return err
	}
	return m.SetClient(skill, strings.TrimSpace(id), secret)
}

func (c connections) Disconnect(ctx context.Context, skill string) error {
	if c.a.oauth == nil {
		return oauth.ErrUnknown
	}
	return c.a.oauth.Disconnect(ctx, skill)
}
