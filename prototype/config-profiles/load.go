package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// load.go — parse the JSON fixture set into a SourceSet. A fixture file is
// either a single profile, a single layering_policy, or a {profiles,policies}
// bundle; the "kind" field disambiguates. Re-parsing the same directory yields
// an identical SourceSet (the H7 reproducibility guarantee starts here).

type rawUnit struct {
	Kind string `json:"kind"`
}

// LoadSourceSet reads every *.json file in dir, sorted by filename so parse
// order is deterministic regardless of filesystem iteration order.
func LoadSourceSet(dir string) (SourceSet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return SourceSet{}, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var src SourceSet
	for _, name := range names {
		if err := loadFileInto(&src, filepath.Join(dir, name)); err != nil {
			return SourceSet{}, err
		}
	}
	return src, nil
}

func loadFileInto(src *SourceSet, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var probe rawUnit
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	switch probe.Kind {
	case "profile":
		var p Profile
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		src.Profiles = append(src.Profiles, p)
	case "layering_policy":
		var p LayeringPolicy
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		src.Policies = append(src.Policies, p)
	default:
		var bundle SourceSet
		if err := json.Unmarshal(data, &bundle); err != nil {
			return err
		}
		src.Profiles = append(src.Profiles, bundle.Profiles...)
		src.Policies = append(src.Policies, bundle.Policies...)
	}
	return nil
}
