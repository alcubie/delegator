// Package handler runs agents for delegator through the Agent Client
// Protocol. It starts an agent's ACP command, opens or loads a session in a
// directory, sends a prompt, answers permission requests by a policy, serves
// file reads and writes, and turns the session updates into one stream of
// events, whatever the agent.
//
// It is built next to internal/adapters and replaces it piece by piece: the
// adapters package keeps driving claude's command line until each part of
// this one is ready.
package handler
