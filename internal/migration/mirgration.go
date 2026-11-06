package migration

// Run migrates the configuration and data left by older nali versions.
// It must be called before the config is read.
func Run() {
	migration2v6()
	migration2v8()
	migration2v9()
}
