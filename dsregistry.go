package main

// dsregistry.go -- v0.60 architecture block 4: the datastore's
// managed-file table, registered into internal/datastore from
// here. The table lives in package main because its validators
// parse into domain types (mjSettings, nodeStore, sshConn,
// ...); the data layer itself must never import the domains it
// serves, so registration is the seam. Entries are exactly the
// pre-v0.60 dsRegistry literal -- paths, schema versions, file
// permissions, backup policy and validators unchanged, so no
// Data file changes by a single byte.

import (
	"encoding/json"
	"fmt"

	"mjlight/internal/datastore"
)

func init() {
	for name, spec := range dsSpecs {
		datastore.Register(name, spec)
	}
}

var dsSpecs = map[string]datastore.Spec{
	"settings":   {Rel: "settings.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error { var s mjSettings; return json.Unmarshal(b, &s) }},
	"nodes":      {Rel: "nodes.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error { var s nodeStore; return json.Unmarshal(b, &s) }},
	"links":      {Rel: "links.txt", Version: 1, Perm: 0o600, Backup: true},
	"webapps":    {Rel: "apps.txt", Version: 1, Perm: 0o644, Backup: true},
	"tombstones": {Rel: "apps-deleted.txt", Version: 1, Perm: 0o644, Backup: true},
	"layout": {Rel: "layout.json", Version: 1, Perm: 0o644, Backup: true, Validate: func(b []byte) error {
		if !json.Valid(b) {
			return fmt.Errorf("invalid JSON")
		}
		return nil
	}},
	"wingeom":       {Rel: "wingeom.json", Version: 1, Perm: 0o644, Backup: true, Validate: func(b []byte) error { var m map[string]winGeom; return json.Unmarshal(b, &m) }},
	"notifications": {Rel: "notifications.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error { var f notifyFile; return json.Unmarshal(b, &f) }},
	"remote": {Rel: "remote.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error {
		var f struct {
			Operators []remoteOperator `json:"operators"`
		}
		return json.Unmarshal(b, &f)
	}},
	"ssh-connections":  {Rel: "ssh/connections.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error { var c []sshConn; return json.Unmarshal(b, &c) }},
	"servermon-config": {Rel: "servermon/config.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error { var m map[string]monCfg; return json.Unmarshal(b, &m) }},
	"servermon-usage":  {Rel: "servermon/usage.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error { var m map[string]*monUsage; return json.Unmarshal(b, &m) }},
	"apps-registry":    {Rel: "apps/apps.json", Version: 1, Perm: 0o644, Backup: true, Validate: func(b []byte) error { var m map[string]installedApp; return json.Unmarshal(b, &m) }},
	"sandbox-apps":     {Rel: "sandbox/apps.json", Version: 1, Perm: 0o644, Backup: true, Validate: func(b []byte) error { var m map[string]boxApp; return json.Unmarshal(b, &m) }},
	"sbie-meta":        {Rel: "components/sandboxie.json", Version: 1, Perm: 0o644, Backup: true, Validate: func(b []byte) error { var m sbieMeta; return json.Unmarshal(b, &m) }},
	"oc-prefs":         {Rel: "openclaw/mj-prefs.json", Version: 1, Perm: 0o644, Backup: true, Validate: func(b []byte) error { var m map[string]json.RawMessage; return json.Unmarshal(b, &m) }},
	"ffstate":          {Rel: "ffstate.json", Version: 1, Perm: 0o644, Backup: true, Validate: func(b []byte) error { var r ffProcRecord; return json.Unmarshal(b, &r) }},
	"accel":            {Rel: "accel.txt", Version: 1, Perm: 0o600, Backup: true},
	"update-token":     {Rel: "update-token.txt", Version: 1, Perm: 0o600, Backup: false},
	// v0.59: the updater's memory of which release tag the
	// running build was installed from (drives the beta
	// channel's offer rule; written by the apply handover).
	"update-state": {Rel: "update-state.json", Version: 1, Perm: 0o600, Backup: true, Validate: func(b []byte) error { var s updateStateFile; return json.Unmarshal(b, &s) }},
	"ff-version":   {Rel: "firefox-version.txt", Version: 1, Perm: 0o644, Backup: true},
	"oc-version":   {Rel: "openclaw-version.txt", Version: 1, Perm: 0o644, Backup: true},
}
