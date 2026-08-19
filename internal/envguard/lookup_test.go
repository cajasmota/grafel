package envguard

import "os"

// lookupEnvFunc is os.LookupEnv as an Env, so a test can run Check against the
// real process environment.
var lookupEnvFunc Env = os.LookupEnv
