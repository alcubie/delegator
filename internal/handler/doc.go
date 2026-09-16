// Package handler runs agents for delegator through the Agent Client
// Protocol. It starts an agent's ACP command, opens or loads a session in a
// directory, sends a prompt, answers permission requests by a policy, serves
// file reads and writes, and turns the session updates into one stream of
// events, whatever the agent.
package handler
