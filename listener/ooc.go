package listener

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/xackery/discordeq/discord"
	"github.com/xackery/eqemuconfig"
	"github.com/ziutek/telnet"
)

var newTelnet bool

var channelID string

var config *eqemuconfig.Config

var t *telnet.Conn

// GetTelnet returns the telnet connection
func GetTelnet() (conn *telnet.Conn) {
	conn = t
	return
}

// ListenToOOC listens to telnet for OOC messages
func ListenToOOC(eqconfig *eqemuconfig.Config, disco *discord.Discord) {
	var err error
	config = eqconfig
	channelID = config.Discord.ChannelID

	if err = connectTelnet(config); err != nil {
		log.Println("[OOC] connectTelnet:", err.Error())
		return
	}

	if err = checkForMessages(config, t, disco); err != nil {
		log.Println("[OOC] checkForMessages:", err.Error())
	}
	t.Close()
	t = nil
	return
}

func connectTelnet(config *eqemuconfig.Config) (err error) {
	if t != nil {
		return
	}
	ip := config.World.Telnet.Ip
	if ip == "" {
		ip = config.World.Tcp.Ip
	}
	port := config.World.Telnet.Port
	if port == "" {
		port = config.World.Tcp.Port
	}

	log.Printf("[OOC] Connecting to %s:%s...\n", ip, port)

	if t, err = telnet.Dial("tcp", fmt.Sprintf("%s:%s", ip, port)); err != nil {
		err = errors.Wrap(err, "telnet dial")
		return
	}
	t.SetReadDeadline(time.Now().Add(10 * time.Second))
	t.SetWriteDeadline(time.Now().Add(10 * time.Second))
	index := 0
	skipAuth := false
	if index, err = t.SkipUntilIndex("Username:", "Connection established from localhost, assuming admin"); err != nil {
		err = errors.Wrap(err, "telnet username/skip wait")
		return
	}
	if index != 0 {
		skipAuth = true
		log.Println("[OOC] Skipping auth")
		newTelnet = true
	}

	if !skipAuth {
		if err = Sendln(config.Discord.TelnetUsername); err != nil {
			err = errors.Wrap(err, "telnet username send")
			return
		}

		if err = t.SkipUntil("Password:"); err != nil {
			err = errors.Wrap(err, "telnet password wait")
			return
		}
		if err = Sendln(config.Discord.TelnetPassword); err != nil {
			err = errors.Wrap(err, "telnet password send")
			return
		}
	}

	if err = Sendln("echo off"); err != nil {
		err = errors.Wrap(err, "echo off send")
		return
	}

	if err = Sendln("acceptmessages on"); err != nil {
		err = errors.Wrap(err, "acceptmessages on send")
		return
	}

	t.SetReadDeadline(time.Time{})
	t.SetWriteDeadline(time.Time{})
	log.Printf("[OOC] Connected\n")
	return
}

// Sendln sends a message to telnet
func Sendln(s string) (err error) {

	buf := make([]byte, len(s)+1)
	copy(buf, s)
	buf[len(s)] = '\n'
	if t == nil {
		for {
			if err = connectTelnet(config); err != nil {
				err = errors.Wrap(err, "telnet telnetusername send")
				return
			}
			fmt.Println("Telnet not connected, reconnecting...")
			time.Sleep(config.Discord.RefreshRate)
		}
	}
	_, err = t.Write(buf)
	if err != nil {
		err = errors.Wrap(err, "failed to write")
		return
	}
	return
}

func checkForMessages(config *eqemuconfig.Config, t *telnet.Conn, disco *discord.Discord) (err error) {
	data := []byte{}
	message := ""
	for {
		if data, err = t.ReadUntil("\n"); err != nil {
			err = fmt.Errorf("Error reading: %s", err.Error())
			return
		}
		message = string(data)
		//log.Printf("[DEBUG OOC] %s", message)
		if len(message) < 3 { //ignore small messages
			continue
		}
		if !strings.Contains(message, "says ooc,") { //ignore non-ooc
			continue
		}
		if strings.Index(message, ">") > 0 && strings.Index(message, ">") < strings.Index(message, " ") { //ignore prompts
			message = message[strings.Index(message, ">")+1:]
		}
		if message[0:1] == "*" { //ignore echo backs
			continue
		}

		sender := message[0:strings.Index(message, " says ooc,")]

		//newTelnet added some odd garbage, this cleans it
		sender = strings.Replace(sender, ">", "", -1) //remove duplicate prompts
		sender = strings.Replace(sender, " ", "", -1) //clean up
		sender = alphanumeric(sender)                 //purify name to be alphanumeric

		padOffset := 3
		if newTelnet { //if new telnet, offsetis 2 off.
			padOffset = 2
		}
		message = message[strings.Index(message, "says ooc, '")+11 : len(message)-padOffset]

		sender = strings.Replace(sender, "_", " ", -1)

		message = convertLinks(config.Discord.ItemUrl, message)

		if _, err = disco.SendMessage(channelID, fmt.Sprintf("**%s:** %s", sender, message)); err != nil {
			errStr := err.Error()
			if strings.Contains(err.Error(), "Unauthorized") {
				errStr = fmt.Sprintf("%s (try visiting: https://discordapp.com/oauth2/authorize?&client_id=%s&scope=bot&permissions=2146958591 to give access)", err.Error(), config.Discord.ClientID)
			}
			log.Printf("[OOC] Error sending message (%s: %s) %s", sender, message, errStr)
			continue
		}
		log.Printf("[OOC] %s: %s\n", sender, message)
	}
}

func convertLinks(prefix string, message string) (messageFixed string) {
	messageFixed = message
	if strings.Count(message, "") > 1 {
		sets := strings.SplitN(message, "", 3)

		itemid, err := strconv.ParseInt(sets[1][0:6], 16, 32)
		if err != nil {
			itemid = 0
		}
		itemname := sets[1][56:]
		itemlink := prefix
		if itemid > 0 && len(prefix) > 0 {
			itemlink = fmt.Sprintf(" %s%d (%s)", itemlink, itemid, itemname)
		} else {
			itemlink = fmt.Sprintf(" *%s* ", itemname)
		}
		messageFixed = sets[0] + itemlink + sets[2]
		if strings.Count(message, "") > 1 {
			messageFixed = convertLinks(prefix, messageFixed)
		}
	}
	return
}
