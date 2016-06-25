package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/jasonlvhit/gocron"
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/telegram-bot-api.v4"
	"log"
	"net/http"
	"os"
	"time"
)

type Episodes struct {
	Episodes []Episode
}

type Episode struct {
	Name          string
	EpisodeNumber int
	EpisodeSeason int
	Serie         Serie
}

type Serie struct {
	Name string
}

const api_url string = "https://feedmyaddiction.xyz/api/v1/daily/"

func main() {

	bot, err := tgbotapi.NewBotAPI(os.Getenv("API_KEY"))
	if err != nil {
		log.Panic(err)
	}

	db, err := sql.Open("sqlite3", "registry.db")
	if err != nil {
		log.Panic(err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS subs (id integer not null primary key, channel integer, user integer)")
	if err != nil {
		log.Panic(err)
	}

	//bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	gocron.Every(1).Day().At("06:01").Do(broadcastToSubscribers, bot, db)

	go gocron.Start()

	getUpdates(bot, db)
}

func getJson(url string, target interface{}) error {

	auth := []byte(os.Getenv("AUTH"))

	client := &http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString(auth)))

	r, err := client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}

func broadcastToSubscribers(bot *tgbotapi.BotAPI, db *sql.DB) {
	log.Printf("Broadcasting to all subscribers..")
	rows, err := db.Query("select id, channel, user from subs")
	if err != nil {
		log.Panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var channel int64
		var user string
		err = rows.Scan(&id, &channel, &user)
		if err != nil {
			log.Panic(err)
		}
		log.Printf("Broadcasting to %d", channel)

		url := fmt.Sprintf("%s%s", api_url, user)
		data := Episodes{}
		err = getJson(url, &data)
		if err != nil {
			log.Panic(err)
			log.Printf("Failed broadcasting to %d", channel)
			continue
		}

		text := "Airing today:\n"
		for _, episode := range data.Episodes {
			text += fmt.Sprintf("- S%02dE%02d %s\n", episode.EpisodeSeason, episode.EpisodeNumber, episode.Serie.Name)
		}
		bot.Send(tgbotapi.NewMessage(channel, text))

		time.Sleep(2 * time.Second)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
}

func getUpdates(bot *tgbotapi.BotAPI, db *sql.DB) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)
	if err != nil {
		log.Panic(err)
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		cmd := update.Message.Command()
		channel := update.Message.Chat.ID

		switch cmd {
		case "today":
			data := Episodes{}
			err = getJson(api_url, &data)
			if err != nil {
				log.Panic(err)
			}
			text := "Airing today:\n"
			for _, episode := range data.Episodes {
				text += fmt.Sprintf("- S%02dE%02d %s\n", episode.EpisodeSeason, episode.EpisodeNumber, episode.Serie.Name)
			}
			msg := tgbotapi.NewMessage(channel, text)
			bot.Send(msg)
		case "sub":
			args := update.Message.CommandArguments()
			log.Printf("[%d] wants to subscribe on %s", channel, args)
			unsubscribe(db, channel)
			subscribe(db, channel, args)
			bot.Send(tgbotapi.NewMessage(channel, fmt.Sprintf("You have subscribed on the track list of %s \nThis channel will now receive daily updates", args)))
		case "unsub":
			log.Printf("[%d] wants to unsubscribe", update.Message.Chat.ID)
			unsubscribe(db, update.Message.Chat.ID)
			bot.Send(tgbotapi.NewMessage(channel, "This channel will no longer receive any updates"))
		default:
			log.Printf("No command")
		}

	}
}

func subscribe(db *sql.DB, channel int64, commandArg string) {
	_, err := db.Exec("INSERT INTO subs (id, channel, user) VALUES(null, ?, ?)", channel, commandArg)
	if err != nil {
		log.Panic(err)
	}
}

func unsubscribe(db *sql.DB, channel int64) {
	_, err := db.Exec("DELETE FROM subs WHERE channel = ?", channel)
	if err != nil {
		log.Panic(err)
	}
}
