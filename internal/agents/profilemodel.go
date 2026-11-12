package agents

// Model is the --model value of the profile's command line, or "".
func (p Profile) Model() string { return flagValue(p.Argv, "--model") }

// Effort is the --effort value of the profile's command line, or "".
func (p Profile) Effort() string { return flagValue(p.Argv, "--effort") }
