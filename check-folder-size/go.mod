module check-folder-size

go 1.24.4

require (
	common-module v0.0.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/term v0.40.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace common-module => ../common-module
