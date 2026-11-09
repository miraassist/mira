package ws

import (
	"context"
	"encoding/json"
)

// CmdMQTTSubscribe subscribes to an MQTT topic through Home Assistant's MQTT
// integration (an administrator's command; tried with the owner's token on
// the test HA 2026-11-09). Retained messages come at once.
const CmdMQTTSubscribe = "mqtt/subscribe"

// MQTTMessage is one message of an MQTT subscription.
type MQTTMessage struct {
	Topic   string `json:"topic"`
	Payload string `json:"payload"`
	Retain  bool   `json:"retain"`
}

// SubscribeMQTT subscribes to an MQTT topic (wildcards allowed) through Home
// Assistant, so Mira needs no MQTT client of her own. The handler runs on the
// connection's read loop: it only passes the message on. The returned
// function ends the subscription.
func (c *Client) SubscribeMQTT(ctx context.Context, topic string, handler func(MQTTMessage)) (func(), error) {
	return c.subscribeCommand(ctx, map[string]any{"type": CmdMQTTSubscribe, "topic": topic}, func(ev Event) {
		var m MQTTMessage
		if err := json.Unmarshal(ev.Raw, &m); err != nil {
			c.log.Warn("undecodable MQTT message", "topic", topic, "error", err)
			return
		}
		handler(m)
	})
}
