module git.jba.io/go/wiki

go 1.14

require (
	git.jba.io/go/auth v1.0.9
	git.jba.io/go/httputils v0.0.0-20190322205649-639279c6da32
	git.jba.io/go/wiki/vfs/assets v0.0.0-20191215031851-8d47f34bdc41
	git.jba.io/go/wiki/vfs/templates v0.0.0-00010101000000-000000000000
	github.com/alcortesm/tgz v0.0.0-20161220082320-9c5fe88206d7 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20161002113705-648efa622239 // indirect
	github.com/dimfeld/httptreemux v5.0.0+incompatible
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/gliderlabs/ssh v0.2.2 // indirect
	github.com/google/go-cmp v0.3.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/justinas/alice v0.0.0-20171023064455-03f45bd4b7da
	github.com/kevinburke/ssh_config v0.0.0-20180127194858-0ff8514904a8 // indirect
	github.com/kr/pretty v0.1.0 // indirect
	github.com/oxtoacart/bpool v0.0.0-20150712133111-4e1c5567d7c2
	github.com/pelletier/go-buffruneio v0.2.0 // indirect
	github.com/pelletier/go-toml v1.2.0
	github.com/renstrom/fuzzysearch v1.0.1
	github.com/russross/blackfriday v1.5.2
	github.com/sergi/go-diff v1.0.0 // indirect
	github.com/sirupsen/logrus v1.4.2
	github.com/src-d/gcfg v1.3.0 // indirect
	github.com/tevjef/go-runtime-metrics v0.0.0-20170326170900-527a54029307
	github.com/xanzy/ssh-agent v0.1.0 // indirect
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gopkg.in/src-d/go-billy.v4 v4.0.2 // indirect
	gopkg.in/src-d/go-git-fixtures.v3 v3.5.0 // indirect
	gopkg.in/src-d/go-git.v4 v4.1.0
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.2.2
)

replace (
	git.jba.io/go/wiki/vfs => ./vfs
	git.jba.io/go/wiki/vfs/assets => ./vfs/assets
	git.jba.io/go/wiki/vfs/templates => ./vfs/templates
)
