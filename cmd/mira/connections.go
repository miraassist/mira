package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
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
	if err := literal(c.a.lang, map[string]string{skills.SettingClientID: id}); err != nil {
		return err
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

// RemoteAddress is server.url as mira.yaml has it, and whether a skill
// that is on has a remote agent (docs/spec/remote-agents.md, decision 13).
func (c connections) RemoteAddress() (string, bool) {
	used := c.a.remote != nil && len(c.a.remote.Agents()) > 0
	address := c.a.liveConfig().Server.URL
	// What the file holds, saved and not yet in force after a restart.
	if e := c.a.edit; e != nil {
		if doc, err := e.file.Read(); err == nil && len(doc.Content) > 0 {
			address = ""
			if v := config.MapGet(config.MapGet(doc.Content[0], "server"), "url"); v != nil {
				address = v.Value
			}
		}
	}
	return address, used
}

// SaveRemoteAddress writes server.url to mira.yaml (empty removes it),
// checked as Mira checks it when she starts; it holds after a restart.
func (c connections) SaveRemoteAddress(ctx context.Context, address string) error {
	e := c.a.edit
	if e == nil {
		return errors.New(i18n.Pick(remoteAddressTexts, c.a.lang).noFile)
	}
	address = strings.TrimRight(address, "/")
	if err := literal(c.a.lang, map[string]string{"url": address}); err != nil {
		return err
	}
	if address != "" {
		u, err := url.Parse(address)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf(i18n.Pick(remoteAddressTexts, c.a.lang).wrong, address)
		}
	}
	return e.write(nil, func(top *yaml.Node) error {
		server := config.MapChild(top, "server")
		if address == "" {
			config.MapDelete(server, "url")
			return nil
		}
		config.MapSet(server, "url", config.Scalar(address))
		return nil
	}, true)
}

type remoteAddressText struct{ noFile, wrong string }

var remoteAddressTexts = map[i18n.Lang]remoteAddressText{
	i18n.DE: {noFile: "Die mira.yaml lässt sich hier nicht ändern.",
		wrong: "„%s“ geht nicht: Die Adresse beginnt mit http:// oder https://, dann kommen Name oder IP-Adresse und der Port, etwa http://192.168.1.10:8787."},
	i18n.EN: {noFile: "mira.yaml cannot be changed here.",
		wrong: "\"%s\" does not work: the address starts with http:// or https://, then the name or IP address and the port, for example http://192.168.1.10:8787."},
}
