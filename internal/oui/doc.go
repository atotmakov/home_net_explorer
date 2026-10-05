// Package oui maps MAC address prefixes to manufacturers using the IEEE registry embedded at
// build time (oui.tsv.gz). The table is refreshed at release time, never at runtime.
package oui

//go:generate go run ../../tools/gen-oui -out oui.tsv.gz
