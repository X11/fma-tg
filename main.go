package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/jasonlvhit/gocron"
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/telegram-bot-api.v4"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Episodes struct {
	Episodes []Episode
}

type Episode struct {
	ID            int
	Name          string
	EpisodeNumber int
	EpisodeSeason int
	Serie         Serie
}

type Serie struct {
	ID       int
	Name     string
	Overview string
}

const api_url string = "https://feedmyaddiction.xyz/api/v1/"

func main() {

	bot, err := tgbotapi.NewBotAPI(os.Getenv("API_KEY"))
	if err != nil {
		log.Panic(err)
	}

	path := "registry.db"
	if os.Getenv("BOT_ENV") == "production" {
		path = "/data/registry.db"
	}

	db, err := sql.Open("sqlite3", path)
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

	auth := "?api_token=" + os.Getenv("AUTH")

	client := &http.Client{}

	req, err := http.NewRequest("GET", url+auth, nil)
	if err != nil {
		return err
	}

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

		url := fmt.Sprintf("%sdaily/%s", api_url, user)
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
		handleMessage(bot, db, &update)
		handleCallbackQuery(bot, db, &update)
	}
}

func handleCallbackQuery(bot *tgbotapi.BotAPI, db *sql.DB, update *tgbotapi.Update) {
	if update.CallbackQuery == nil {
		return
	}

	parts := strings.Split(update.CallbackQuery.Data, "=")
	switch parts[0] {
	case "serie":
		bot.AnswerCallbackQuery(tgbotapi.NewCallback(update.CallbackQuery.ID, "Fetching information"))

		data := Serie{}
		err := getJson(api_url+"serie/"+parts[1], &data)
		if err != nil {
			log.Printf("Not able to get serie data, something went wrong")
		}
		msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, data.Name+"\n\n"+data.Overview)
		bot.Send(msg)
	}
}

func handleMessage(bot *tgbotapi.BotAPI, db *sql.DB, update *tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	cmd := update.Message.Command()
	channel := update.Message.Chat.ID

	switch cmd {
	case "today":
		data := Episodes{}
		err := getJson(api_url+"daily/", &data)
		if err != nil {
			log.Printf("Not able to get todays data, something went wrong")
		}
		text := "Airing today:\n"
		for _, episode := range data.Episodes {
			text += fmt.Sprintf("- S%02dE%02d %s\n", episode.EpisodeSeason, episode.EpisodeNumber, episode.Serie.Name)
		}
		msg := tgbotapi.NewMessage(channel, text)
		returnMsg, err := bot.Send(msg)
		if err != nil {
			log.Printf("Something went wrong with sending chat message")
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup([]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonURL("View calender", "https://feedmyaddiction.xyz/calender/"),
		})
		keyboardMsg := tgbotapi.NewEditMessageReplyMarkup(channel, returnMsg.MessageID, keyboard)
		bot.Send(keyboardMsg)

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

	case "search":
		args := update.Message.CommandArguments()

		if len(args) < 5 {
			msg := tgbotapi.NewMessage(channel, "Specify atleast 5 characters while searching")
			bot.Send(msg)
			return
		}
		data := Episodes{}
		err := getJson(api_url+"search/"+args, &data)
		if err != nil {
			log.Printf("Not able to get todays data, something went wrong")
		}

		msg := tgbotapi.NewMessage(channel, "Search results")
		returnMsg, err := bot.Send(msg)
		if err != nil {
			return
		}

		rows := [][]tgbotapi.InlineKeyboardButton{}
		for _, episode := range data.Episodes {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData(episode.Serie.Name, "serie="+strconv.Itoa(episode.Serie.ID)),
			})
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
		keyboardMsg := tgbotapi.NewEditMessageReplyMarkup(channel, returnMsg.MessageID, keyboard)
		bot.Send(keyboardMsg)
	default:
		log.Printf("No command")
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
