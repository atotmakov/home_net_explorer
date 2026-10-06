// Package collect is the scan engine: it plans subnets from the vantage report, probes them
// in parallel, resolves names, and builds a contract.CollectionRun. Network access goes
// through the Prober, NeighborTable, RouteReader and Resolver interfaces.
package collect
