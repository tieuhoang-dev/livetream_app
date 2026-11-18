package config

import "livesteam_app/internal/realtime"

// Realtime is the application event bus shared by all domain services.
var Realtime = realtime.New(nil)
