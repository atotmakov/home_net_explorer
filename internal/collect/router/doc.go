// Package router reads device lists from home routers (feature 002). Router sources are
// opt-in and configured only in a remote collector's hne-collector.json. Access is read-only
// (log in, read the list, log out), and the router login never leaves the collector machine:
// it is not uploaded, logged or printed (FR-006).
package router
