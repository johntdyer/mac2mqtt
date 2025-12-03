// main
package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	mediadevices "github.com/antonfisher/go-media-devices-state"
	"github.com/elastic/gosigar"
	"github.com/polera/publicip"
	"gopkg.in/yaml.v2"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var hostname string

type config struct {
	MqttIP           string `yaml:"mqtt_ip"`
	Port             string `yaml:"mqtt_port"`
	User             string `yaml:"mqtt_user"`
	Password         string `yaml:"mqtt_password"`
	VolumeInterval   int    `yaml:"volume_interval"`
	BatteryInterval  int    `yaml:"battery_interval"`
	CPUInterval      int    `yaml:"cpu_interval"`
	MemoryInterval   int    `yaml:"memory_interval"`
	UptimeInterval   int    `yaml:"uptime_interval"`
	PublicIPInterval int    `yaml:"publicip_interval"`
	DiskInterval     int    `yaml:"disk_interval"`
	MediaInterval    int    `yaml:"media_interval"`
}

func (c *config) getConfig() *config {

	configContent, err := ioutil.ReadFile("mac2mqtt.yaml")
	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(configContent, c)
	if err != nil {
		log.Fatal(err)
	}

	if c.MqttIP == "" {
		log.Fatal("Must specify mqtt_ip in mac2mqtt.yaml")
	}

	if c.Port == "" {
		log.Fatal("Must specify mqtt_port in mac2mqtt.yaml")
	}

	if c.User == "" {
		log.Fatal("Must specify mqtt_user in mac2mqtt.yaml")
	}

	if c.Password == "" {
		log.Fatal("Must specify mqtt_password in mac2mqtt.yaml")
	}

	// Set default intervals if not specified
	if c.VolumeInterval == 0 {
		c.VolumeInterval = 5
	}
	if c.BatteryInterval == 0 {
		c.BatteryInterval = 60
	}
	if c.CPUInterval == 0 {
		c.CPUInterval = 10
	}
	if c.MemoryInterval == 0 {
		c.MemoryInterval = 10
	}
	if c.UptimeInterval == 0 {
		c.UptimeInterval = 60
	}
	if c.PublicIPInterval == 0 {
		c.PublicIPInterval = 300
	}
	if c.DiskInterval == 0 {
		c.DiskInterval = 60
	}
	if c.MediaInterval == 0 {
		c.MediaInterval = 5
	}

	return c
}

func getHostname() string {

	hostname, err := os.Hostname()

	if err != nil {
		log.Fatal(err)
	}

	// "name.local" => "name"
	firstPart := strings.Split(hostname, ".")[0]

	// remove all symbols, but [a-zA-Z0-9_-]
	reg, err := regexp.Compile("[^a-zA-Z0-9_-]+")
	if err != nil {
		log.Fatal(err)
	}
	firstPart = reg.ReplaceAllString(firstPart, "")

	return firstPart
}

func getCommandOutput(name string, arg ...string) string {
	cmd := exec.Command(name, arg...)

	stdout, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	stdoutStr := string(stdout)
	stdoutStr = strings.TrimSuffix(stdoutStr, "\n")

	return stdoutStr
}

func getMuteStatus() bool {
	output := getCommandOutput("/usr/bin/osascript", "-e", "output muted of (get volume settings)")
	if output == "missing value" {
		return false
	}

	b, err := strconv.ParseBool(output)
	if err != nil {
		log.Fatal(err)
	}

	return b
}

func getCurrentVolume() int {
	output := getCommandOutput("/usr/bin/osascript", "-e", "output volume of (get volume settings)")
	if output == "missing value" {
		return -1
	}

	i, err := strconv.Atoi(output)
	if err != nil {
		log.Fatal(err)
	}

	return i
}

func getCurrentInputVolume() int {
	input := getCommandOutput("/usr/bin/osascript", "-e", "input volume of (get volume settings)")

	i, err := strconv.Atoi(input)
	if err != nil {
		log.Fatal(err)
	}

	return i
}

func runCommand(name string, arg ...string) {
	cmd := exec.Command(name, arg...)

	_, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
}

// from 0 to 100
func setVolume(i int) {
	runCommand("/usr/bin/osascript", "-e", "set volume output volume "+strconv.Itoa(i))
}

// from 0 to 100
func setInputVolume(i int) {
	runCommand("/usr/bin/osascript", "-e", "set volume input volume "+strconv.Itoa(i))
}

// true - turn mute on
// false - turn mute off
func setMute(b bool) {
	runCommand("/usr/bin/osascript", "-e", "set volume output muted "+strconv.FormatBool(b))
}

func commandSleep() {
	runCommand("pmset", "sleepnow")
}

func commandDisplaySleep() {
	runCommand("pmset", "displaysleepnow")
}

func commandDisplayLock() {
	runCommand("/usr/bin/osascript", "-e", "tell application \"System Events\" to tell process \"Finder\" to keystroke \"q\" using {control down, command down}")
}

func commandDisplayWake() {
	runCommand("/usr/bin/caffeinate", "-u", "-t", "1")
}

func commandShutdown() {
	if os.Getuid() == 0 {
		// if the program is run by root user we are doing the most powerfull shutdown - that always shuts down the computer
		runCommand("shutdown", "-h", "now")
	} else {
		// if the program is run by ordinary user we are trying to shutdown, but it may fail if the other user is logged in
		runCommand("/usr/bin/osascript", "-e", "tell app \"System Events\" to shut down")
	}
}

func commandRunShortcut(shortcut string) {
	runCommand("shortcuts", "run", shortcut)
}

var _ mqtt.MessageHandler = func(_ mqtt.Client, msg mqtt.Message) {
	log.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	log.Println("Connected to MQTT")

	token := client.Publish(getTopicPrefix()+"/status/alive", 0, true, "true")
	token.Wait()

	log.Println("Sending 'true' to topic: " + getTopicPrefix() + "/status/alive")

	listen(client, getTopicPrefix()+"/command/#")
}

var connectLostHandler mqtt.ConnectionLostHandler = func(_ mqtt.Client, err error) {
	log.Printf("Disconnected from MQTT: %v", err)
}

func getMQTTClient(ip, port, user, password string) mqtt.Client {

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%s", ip, port))
	opts.SetUsername(user)
	opts.SetPassword(password)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler

	opts.SetWill(getTopicPrefix()+"/status/alive", "false", 0, true)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	return client
}

func getTopicPrefix() string {
	return "mac2mqtt/" + hostname
}

func listen(client mqtt.Client, topic string) {

	token := client.Subscribe(topic, 0, func(client mqtt.Client, msg mqtt.Message) {

		if msg.Topic() == getTopicPrefix()+"/command/volume" {

			i, err := strconv.Atoi(string(msg.Payload()))
			if err == nil && i >= 0 && i <= 100 {

				setVolume(i)

				updateVolume(client)
				updateMute(client)

			} else {
				log.Println("Incorrect value")
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/input_volume" {

			i, err := strconv.Atoi(string(msg.Payload()))
			if err == nil && i >= 0 && i <= 100 {

				setInputVolume(i)

				updateInputVolume(client)
				// updateMute(client)

			} else {
				log.Println("Incorrect value")
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/mute" {

			b, err := strconv.ParseBool(string(msg.Payload()))
			if err == nil {
				setMute(b)

				updateVolume(client)
				updateMute(client)

			} else {
				log.Println("Incorrect value")
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/sleep" {

			if string(msg.Payload()) == "sleep" {
				commandSleep()
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/displaysleep" {

			if string(msg.Payload()) == "displaysleep" {
				commandDisplaySleep()
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/displaylock" {

			if string(msg.Payload()) == "displaylock" {
				commandDisplayLock()
			}

			if string(msg.Payload()) == "displaylock_sleep" {
				commandDisplayLock()
				commandDisplaySleep()
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/displaywake" {

			if string(msg.Payload()) == "displaywake" {
				commandDisplayWake()
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/shutdown" {

			if string(msg.Payload()) == "shutdown" {
				commandShutdown()
			}

		}

		if msg.Topic() == getTopicPrefix()+"/command/runshortcut" {
			commandRunShortcut(string(msg.Payload()))
		}

	})

	token.Wait()
	if token.Error() != nil {
		log.Printf("Token error: %s\n", token.Error())
	}
}

func updateVolume(client mqtt.Client) {
	token := client.Publish(getTopicPrefix()+"/status/volume", 0, false, strconv.Itoa(getCurrentVolume()))
	token.Wait()
}

func updateInputVolume(client mqtt.Client) {
	token := client.Publish(getTopicPrefix()+"/status/input_volume", 0, false, strconv.Itoa(getCurrentInputVolume()))
	token.Wait()
}

func updateMute(client mqtt.Client) {
	token := client.Publish(getTopicPrefix()+"/status/mute", 0, false, strconv.FormatBool(getMuteStatus()))
	token.Wait()
}

func getBatteryChargePercent() string {

	output := getCommandOutput("/usr/bin/pmset", "-g", "batt")

	// $ /usr/bin/pmset -g batt
	// Now drawing from 'Battery Power'
	//  -InternalBattery-0 (id=4653155)        100%; discharging; 20:00 remaining present: true

	r := regexp.MustCompile(`(\d+)%`)
	percent := r.FindStringSubmatch(output)[1]

	return percent
}

func updateBattery(client mqtt.Client) {
	token := client.Publish(getTopicPrefix()+"/status/battery", 0, false, getBatteryChargePercent())
	token.Wait()
}

func getCPUUsage() string {
	cpu1stCheck := gosigar.Cpu{}
	err := cpu1stCheck.Get()
	if err != nil {
		log.Printf("Error getting CPU info: %v", err)
		return "0"
	}

	// Calculate total and idle time
	total := cpu1stCheck.User + cpu1stCheck.Nice + cpu1stCheck.Sys + cpu1stCheck.Idle + cpu1stCheck.Wait
	idle := cpu1stCheck.Idle

	// Sleep briefly to get a second reading for calculation
	time.Sleep(500 * time.Millisecond)

	cpu2ndCheck := gosigar.Cpu{}
	err = cpu2ndCheck.Get()
	if err != nil {
		log.Printf("Error getting CPU info: %v", err)
		return "0"
	}

	total2 := cpu2ndCheck.User + cpu2ndCheck.Nice + cpu2ndCheck.Sys + cpu2ndCheck.Idle + cpu2ndCheck.Wait
	idle2 := cpu2ndCheck.Idle

	// Calculate the difference
	totalDelta := total2 - total
	idleDelta := idle2 - idle

	if totalDelta == 0 {
		return "0"
	}

	// Calculate usage percentage
	cpuUsage := 100.0 * (float64(totalDelta) - float64(idleDelta)) / float64(totalDelta)
	return fmt.Sprintf("%.2f", cpuUsage)
}

func updateCPU(client mqtt.Client) {
	token := client.Publish(getTopicPrefix()+"/status/cpu", 0, false, getCPUUsage())
	token.Wait()
}

func getMemoryUsage() (string, string, string) {
	mem := gosigar.Mem{}
	err := mem.Get()
	if err != nil {
		log.Printf("Error getting memory info: %v", err)
		return "0", "0", "0"
	}

	// Convert bytes to GB for readability
	totalGB := float64(mem.Total) / (1024 * 1024 * 1024)
	usedGB := float64(mem.Used) / (1024 * 1024 * 1024)
	freeGB := float64(mem.Free) / (1024 * 1024 * 1024)

	return fmt.Sprintf("%.2f", totalGB), fmt.Sprintf("%.2f", usedGB), fmt.Sprintf("%.2f", freeGB)
}

func updateMemory(client mqtt.Client) {
	total, used, free := getMemoryUsage()
	token := client.Publish(getTopicPrefix()+"/status/memory/total", 0, false, total)
	token = client.Publish(getTopicPrefix()+"/status/memory/used", 0, false, used)
	token = client.Publish(getTopicPrefix()+"/status/memory/free", 0, false, free)
	token.Wait()
}

func getUptime() (string, string) {
	uptime := gosigar.Uptime{}
	err := uptime.Get()
	if err != nil {
		log.Printf("Error getting uptime: %v", err)
		return "unknown", "0"
	}

	// Convert seconds to milliseconds
	uptimeMs := fmt.Sprintf("%d", int(uptime.Length*1000))

	// Convert seconds to a human-readable format
	totalSeconds := int(uptime.Length)
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60

	var uptimeHuman string
	if days > 0 {
		uptimeHuman = fmt.Sprintf("%d days, %d:%02d", days, hours, minutes)
	} else {
		uptimeHuman = fmt.Sprintf("%d:%02d", hours, minutes)
	}

	return uptimeHuman, uptimeMs
}

func updateUptime(client mqtt.Client) {
	uptimeHuman, uptimeMs := getUptime()
	token := client.Publish(getTopicPrefix()+"/status/uptime/human", 0, false, uptimeHuman)
	token = client.Publish(getTopicPrefix()+"/status/uptime/ms", 0, false, uptimeMs)
	token.Wait()
}

func getPublicIP() string {
	ip, err := publicip.GetIP()
	if err != nil {
		log.Printf("Error getting public IP: %v", err)
		return "unknown"
	}
	return ip
}

func updatePublicIP(client mqtt.Client) {
	token := client.Publish(getTopicPrefix()+"/status/publicip", 0, false, getPublicIP())
	token.Wait()
}

func getDiskUsage() (string, string, string, string) {
	fslist := gosigar.FileSystemList{}
	err := fslist.Get()
	if err != nil {
		log.Printf("Error getting disk info: %v", err)
		return "0", "0", "0", "0"
	}

	// Find the root filesystem
	for _, fs := range fslist.List {
		if fs.DirName == "/" {
			usage := gosigar.FileSystemUsage{}
			err := usage.Get(fs.DirName)
			if err != nil {
				log.Printf("Error getting disk usage for /: %v", err)
				return "0", "0", "0", "0"
			}

			// Calculate percentages
			percentUsed := usage.UsePercent() * 100
			percentFree := 100.0 - percentUsed

			// Calculate bytes (usage provides values in KB, convert to bytes)
			bytesUsed := usage.Used * 1024
			bytesFree := usage.Avail * 1024

			return fmt.Sprintf("%.2f", percentUsed),
				fmt.Sprintf("%.2f", percentFree),
				fmt.Sprintf("%d", bytesUsed),
				fmt.Sprintf("%d", bytesFree)
		}
	}

	return "0", "0", "0", "0"
}

func updateDisk(client mqtt.Client) {
	percentUsed, percentFree, bytesUsed, bytesFree := getDiskUsage()
	token := client.Publish(getTopicPrefix()+"/status/disk/percent_used", 0, false, percentUsed)
	token = client.Publish(getTopicPrefix()+"/status/disk/percent_free", 0, false, percentFree)
	token = client.Publish(getTopicPrefix()+"/status/disk/bytes_used", 0, false, bytesUsed)
	token = client.Publish(getTopicPrefix()+"/status/disk/bytes_free", 0, false, bytesFree)
	token.Wait()
}

func getMediaDevicesState() (string, string) {
	micOn := "false"
	cameraOn := "false"

	isMicOn, err := mediadevices.IsMicrophoneOn()
	if err != nil {
		log.Printf("Error getting microphone state: %v", err)
	} else if isMicOn {
		micOn = "true"
	}

	isCameraOn, err := mediadevices.IsCameraOn()
	if err != nil {
		log.Printf("Error getting camera state: %v", err)
	} else if isCameraOn {
		cameraOn = "true"
	}

	return micOn, cameraOn
}

func updateMediaDevices(client mqtt.Client) {
	micOn, cameraOn := getMediaDevicesState()
	token := client.Publish(getTopicPrefix()+"/status/media/microphone", 0, false, micOn)
	token = client.Publish(getTopicPrefix()+"/status/media/camera", 0, false, cameraOn)
	token.Wait()
}

func main() {

	log.Println("Started")

	var c config
	c.getConfig()

	var wg sync.WaitGroup

	hostname = getHostname()
	mqttClient := getMQTTClient(c.MqttIP, c.Port, c.User, c.Password)

	// Run all updates once on startup
	log.Println("Running initial updates...")
	updateVolume(mqttClient)
	updateInputVolume(mqttClient)
	updateMute(mqttClient)
	updateBattery(mqttClient)
	updateCPU(mqttClient)
	updateMemory(mqttClient)
	updateUptime(mqttClient)
	updatePublicIP(mqttClient)
	updateDisk(mqttClient)
	updateMediaDevices(mqttClient)
	log.Println("Initial updates complete")

	volumeTicker := time.NewTicker(time.Duration(c.VolumeInterval) * time.Second)
	batteryTicker := time.NewTicker(time.Duration(c.BatteryInterval) * time.Second)
	cpuTicker := time.NewTicker(time.Duration(c.CPUInterval) * time.Second)
	memoryTicker := time.NewTicker(time.Duration(c.MemoryInterval) * time.Second)
	uptimeTicker := time.NewTicker(time.Duration(c.UptimeInterval) * time.Second)
	publicipTicker := time.NewTicker(time.Duration(c.PublicIPInterval) * time.Second)
	diskTicker := time.NewTicker(time.Duration(c.DiskInterval) * time.Second)
	mediaTicker := time.NewTicker(time.Duration(c.MediaInterval) * time.Second)

	wg.Add(1)
	go func() {
		for {
			select {
			case _ = <-volumeTicker.C:
				updateVolume(mqttClient)
				updateInputVolume(mqttClient)
				updateMute(mqttClient)

			case _ = <-batteryTicker.C:
				updateBattery(mqttClient)

			case _ = <-cpuTicker.C:
				updateCPU(mqttClient)

			case _ = <-memoryTicker.C:
				updateMemory(mqttClient)

			case _ = <-uptimeTicker.C:
				updateUptime(mqttClient)

			case _ = <-publicipTicker.C:
				updatePublicIP(mqttClient)

			case _ = <-diskTicker.C:
				updateDisk(mqttClient)

			case _ = <-mediaTicker.C:
				updateMediaDevices(mqttClient)
			}
		}
	}()

	wg.Wait()

}
