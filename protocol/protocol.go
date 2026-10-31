// Package protocol is the contract between the ops agent and the control
// plane. The agent dials out: it registers once over HTTP, then holds one
// WebSocket session. Both repos import this package, so a change here is a
// change on both sides.
package protocol

import (
	"encoding/json"
	"errors"
)

// Version is the protocol version the agent reports in Hello. Bump it when a
// message changes in a way an older peer cannot read.
const Version = 1

const (
	// RegisterPath trades a one-time install token, sent as
	// "Authorization: Bearer <install token>", for an agent token.
	RegisterPath = "/agent/register"
	// SessionPath upgrades to the WebSocket session, authorized with
	// "Authorization: Bearer <agent token>".
	SessionPath = "/agent/session"
)

// MessageType decides how Message.Data decodes.
type MessageType string

const (
	// TypeHello is the first message the agent sends on every session.
	TypeHello MessageType = "hello"
)

// Message is the WebSocket envelope. A request carries a RequestID, and its
// answer carries the same one.
type Message struct {
	Type      MessageType     `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// NewMessage wraps data in an envelope of the given type.
func NewMessage(messageType MessageType, data any) (Message, error) {
	message := Message{Type: messageType}
	if data == nil {
		return message, nil
	}
	body, err := json.Marshal(data)
	if err != nil {
		return Message{}, err
	}
	message.Data = body
	return message, nil
}

// Decode reads Data into target.
func (m Message) Decode(target any) error {
	if len(m.Data) == 0 {
		return errors.New("message has no data")
	}
	return json.Unmarshal(m.Data, target)
}

// Hello describes the agent and its server.
type Hello struct {
	ProtocolVersion int    `json:"protocol_version"`
	AgentVersion    string `json:"agent_version"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
}

// RegisterRequest is the body of a register call.
type RegisterRequest struct {
	Hello
}

// RegisterResponse answers a register call. The agent stores the token and
// uses it for every session.
type RegisterResponse struct {
	ServerID   string `json:"server_id"`
	AgentToken string `json:"agent_token"`
}
