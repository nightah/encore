// Package actions implements Encore's voice actions independently of any assistant. Adapters (Alexa today, MCP later) translate requests into calls on Service and render the returned Outcome, which carries message IDs rather than finished sentences so each channel can localise and format them.
package actions
