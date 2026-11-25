// Package acceptance is the black-box acceptance suite of asset-discovery-lab
// (spec §16.3, §16.4). Its tests carry the build tag "acceptance" and run
// against a running stack:
//
//	make acceptance                 # every gate, A to E
//	make acceptance-gate GATE=B     # one gate
//
// This file has no build tag so that the package always builds.
package acceptance
