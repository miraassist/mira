package main

import (
	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/music"
)

// musicPlayers are the exposed Music Assistant players of the house
// structure (docs/spec/music-players.md).
func musicPlayers(snap structure.Snapshot) []music.Player {
	out := make([]music.Player, 0, len(snap.MusicPlayers))
	for id := range snap.MusicPlayers {
		area := snap.EntityArea[id]
		out = append(out, music.Player{Entity: id, Name: snap.EntityLabel[id], Names: snap.EntityNames[id],
			Area: area, AreaName: snap.AreaNames[area]})
	}
	return out
}

// groupMembers is a player's attribute group_members: the players of its
// group, itself among them; empty when it is in none.
func groupMembers(attrs map[string]any) []string {
	list, _ := attrs["group_members"].([]any)
	out := make([]string, 0, len(list))
	for _, m := range list {
		if id, ok := m.(string); ok {
			out = append(out, id)
		}
	}
	return out
}
