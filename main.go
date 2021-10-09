package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	dirFlag        = flag.String("dir", "", "Directory to search")
	dirConfirm     = flag.Bool("dirconfirm", false, "Automatically confirm dir is correct")
	channelsFlag   = flag.String("channels", "", "Channels to filter by")
	guildsFlag     = flag.String("guilds", "", "Guilds to filter by")
	botToken       = flag.String("bottoken", "", "Bot token")
	messagesCsv    = "messages.csv"
	channelJson    = "channel.json"
	baseUrl        = "https://discord.com/api/v9/channels/"
	filterChannels = make([]int64, 0)
	filterGuilds   = make([]int64, 0)
)

// ChannelJson is the representation of part of c<number>/channel.json
type ChannelJson struct {
	ID    int64      `json:"id,string"`
	Guild *GuildJson `json:"guild"`
}

// GuildJson is an object found in ChannelJson
type GuildJson struct {
	ID int64 `json:"id,string"`
}

// Channel is used for keeping track of which messages to delete
type Channel struct {
	ID       int64
	Messages []int64
}

func main() {
	flag.Parse()

	filterChannels = parseIntSlice(channelsFlag, ",")
	filterGuilds = parseIntSlice(guildsFlag, ",")

	// Let the user select a directory
	dir := selectDir(true)
	dir = formatDir(dir)
	fmt.Printf("Searching directory \"%s\"\n", dir)

	// Find all messages.csv files in said dir
	validFiles := getFileList(dir)
	validFilesAmt := len(validFiles)
	fmt.Printf("Found %v channel folders\n", validFilesAmt)
	fmt.Printf("Filtering to %v channels\n", len(filterChannels))
	fmt.Printf("Filtering to %v guilds\n", len(filterGuilds))

	if validFilesAmt == 0 {
		fmt.Printf("Couldn't find any %s files, maybe try another directory? "+
			"Make sure you are selecting the messages directory which contains c<number> folders, "+
			"or a c<number> folder itself. Exiting...\n", messagesCsv)
		return
	}

	channels := extractMessageIDs(validFiles)
	deleteForAllChannels(channels)
}

// parseIntSlice will parse flag, separated by delimiter and return a slice
func parseIntSlice(flag *string, delimiter string) []int64 {
	slice := make([]int64, 0)
	if len(*flag) > 0 {
		for _, item := range strings.Split(*flag, delimiter) {
			num, err := strconv.ParseInt(item, 10, 64)
			if err == nil {
				slice = append(slice, num)
			}
		}
	}
	return slice
}

// deleteForAllChannels will delete each message in each channel
func deleteForAllChannels(channels []Channel) {
	for _, channel := range channels {
		for _, message := range channel.Messages {
			err := deleteChannelMessages(baseUrl +
				strconv.FormatInt(channel.ID, 10) +
				"/messages/" +
				strconv.FormatInt(message, 10))
			if err != nil {
				fmt.Printf("Error deleting channel messages: %v\n", err)
			}
		}
	}
}

// deleteChannelMessages will delete a message with the given url
func deleteChannelMessages(url string) error {
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Auth", "Bot "+*botToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Error making request: %v\n", err)
		return err
	}

	remaining := parseStrUnsafe(res.Header.Get("X-RateLimit-Remaining"))
	reset := parseStrUnsafe(res.Header.Get("X-RateLimit-Reset"))

	if remaining == 0 {
		now := time.Now()
		wait := time.Duration(reset-now.Unix()) * time.Second
		log.Printf("Waiting for %v seconds due to rate limit\n", wait)
		time.Sleep(wait)
	}

	log.Printf("Result: %v\n%s\n", res.StatusCode, url)
	return nil
}

func parseStrUnsafe(str string) int64 {
	if str == "" {
		return 1
	}

	parsed, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		panic(err)
	}
	return parsed
}

// getChannelInfo will get the channel.json from the c<number>/channel.json path
func getChannelInfo(file string) (ChannelJson, error) {
	folder := strings.TrimSuffix(file, messagesCsv)
	jsonPath := folder + channelJson

	data, err := ioutil.ReadFile(jsonPath)
	if err != nil {
		return ChannelJson{}, err
	}
	var cJson ChannelJson
	err = json.Unmarshal(data, &cJson)
	if err != nil {
		return ChannelJson{}, err
	}

	return cJson, nil
}

// contains will check if a slice contains an int64
func contains(s []int64, e int64) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// getRightOfDelimiter will get everything to the right of delimiter.
// Using 1 will give you the last result, and using two will give you the last two chunks, and so on.
func getRightOfDelimiter(str string, delimiter string, offset int) string {
	split := strings.Split(str, delimiter)
	fixedOffset := len(split)
	if offset < 1 {
		fixedOffset = 0
		offset = 0
	}
	return strings.Join(split[fixedOffset-offset:], delimiter)
}

// extractMessageIDs will search all given files, and extract messages IDs in matching channels
func extractMessageIDs(files []string) []Channel {
	channels := make([]Channel, 0)
	totalMessages := 0

	for _, file := range files {
		cJson, err := getChannelInfo(file)
		if err != nil {
			log.Printf("Skipping \"%s\" because: %v\n", getRightOfDelimiter(file, "/", 2), err)
			continue
		} else if cJson.Guild == nil && len(filterGuilds) > 0 {
			// TODO: add debug flag
			// log.Printf("Skipping \"%s\" because cJson.Guild is nil\n", getRightOfDelimiter(file, "/", 2))
			continue
		}

		if len(filterChannels) > 0 && !contains(filterChannels, cJson.ID) {
			continue
		}

		if len(filterGuilds) > 0 && !contains(filterGuilds, cJson.Guild.ID) {
			continue
		}

		csvF, err := os.Open(file)
		if err != nil {
			fmt.Printf("Couldn't open file \"%s\": %v\n", file, err)
			continue
		}

		channel := Channel{ID: cJson.ID}

		r := csv.NewReader(csvF)

		for {
			column, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				fmt.Printf("Error reading csv from \"%s\": %v\n", file, err)
				break
			}

			id := column[0] // First column is the message ID
			if id == "ID" {
				continue // The first line is always a descriptor
			}

			idInt, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				fmt.Printf("Error converting \"%s\" to an int: %v\n", id, err)
				continue
			}

			totalMessages += 1
			channel.Messages = append(channel.Messages, idInt)
		}

		channels = append(channels, channel)
	}

	fmt.Printf("Found %v channels with %v messages\n", len(channels), totalMessages)
	return channels
}

// getFileList will look for all the messages.csv files that exist in dir
func getFileList(dir string) []string {
	files := make([]string, 0)

	fileInfos, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(err)
	}

	for _, f := range fileInfos {
		msgFilePath := dir + f.Name() + "/" + messagesCsv

		if !f.IsDir() {
			// If f is not a directory, but is a messages.csv
			if f.Name() == messagesCsv {
				msgFilePath = dir + "/" + messagesCsv
				if checkFileExists(msgFilePath) {
					files = append(files, msgFilePath)
				}
			}
		} else {
			// If f is a directory (hopefully the channel directory)
			if checkFileExists(msgFilePath) {
				files = append(files, msgFilePath)
			}
		}
	}

	return files
}

// checkFileExists will check if a certain path exists, and ensures it is not a directory
func checkFileExists(path string) bool {
	if f, err := os.Stat(path); err == nil {
		return !f.IsDir() // path exists, return true if it is not a directory
	} else if os.IsNotExist(err) {
		return false // file does not exist
	} else { // schrodinger's file
		log.Printf("Error with schrodinger's file: %v\n", err)
		panic(err)
	}
}

// formatDir will append a / to the end of a dir path if it is missing
func formatDir(dir string) string {
	last, _ := utf8.DecodeLastRuneInString(dir)
	if last != '/' {
		dir += "/"
	}
	return dir
}

// selectDir will ask the user for a directory and confirm the directory they chose
func selectDir(firstRun bool) string {
	// If dirConfirm is selected and there's a dir set, choose it automatically
	if *dirConfirm && len(*dirFlag) != 0 {
		return *dirFlag
	}

	var dir string
	if firstRun {
		dir = *dirFlag
	}

	if len(*dirFlag) == 0 {
		fmt.Printf("Select a directory to scan (use . for current): ")
		fmt.Scan(&dir)
	}

	fmt.Printf("Selected directory: \"%s\"\n", dir)
	fmt.Printf("Is this correct? (Y/N): ")

	var correct string
	fmt.Scan(&correct)

	first := strings.ToLower(correct[0:1])
	if first != "y" {
		fmt.Printf("Selected No, trying again.\n")
		return selectDir(false)
	}

	return dir
}
