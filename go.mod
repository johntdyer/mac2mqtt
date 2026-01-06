module bessarabov/mac2mqtt

go 1.23.0

toolchain go1.24.3

require (
	github.com/antonfisher/go-media-devices-state v0.2.0
	github.com/cloudfoundry/gosigar v1.3.112
	github.com/eclipse/paho.mqtt.golang v1.3.5
	github.com/shirou/gopsutil/v3 v3.24.5
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
)

// using my fork until PR #9 is resolved in upstream
replace github.com/antonfisher/go-media-devices-state => github.com/johntdyer/go-media-devices-state v0.0.0-20251204145225-5b3592a6499f
