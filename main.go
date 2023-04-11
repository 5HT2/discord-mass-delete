package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/joho/godotenv"
)

var (
	dirConfirm = flag.Bool("dirconfirm", false, "Automatically confirm dir is correct")
	useSearch  = flag.Bool("usesearch", false, "Parse the value of a search result JSON instead")

	dirFlag      *string
	channelsFlag *string
	guildsFlag   *string
	authorsFlag  *string
	botToken     *string
	userToken    *string

	messagesCsv    = "messages.csv"
	channelJson    = "channel.json"
	baseUrl        = "https://discord.com/api/v9/channels/"
	searchUrlRegex = regexp.MustCompile(`^https?://discord.com/api/v[0-9]+/channels/[0-9]+/messages/search`)

	filterChannels = make([]int64, 0)
	filterGuilds   = make([]int64, 0)
	filterAuthors  = make([]int64, 0)
	retryAttempts  = int64(0)
	retryMessages  = int64(0)
	retryChannels  = make(map[int64]Channel, 0)
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

// SearchResult is used when parsing search results to valid files
type SearchResult struct {
	URL     string `json:"url,omitempty"`
	Content struct {
		Error   *string `json:"error,omitempty"`
		Content *string `json:"content,omitempty"`
	} `json:"content"`
}

// SearchResultContent is used when parsing SearchResult.Content
type SearchResultContent struct {
	Messages [][]struct {
		ID        int64 `json:"id,string"`
		ChannelID int64 `json:"channel_id,string"`
		Author    struct {
			ID int64 `json:"id,string"`
		} `json:"author,omitempty"`
	} `json:"messages,omitempty"`
}

func main() {
	_ = godotenv.Load()

	// Set flags now that we've loaded the env. Necessary to be able to default to env values.
	dirFlag = flag.String("dir", os.Getenv("DISCORD_DIR"), "Directory to search")
	channelsFlag = flag.String("channels", os.Getenv("DISCORD_CHANNELS"), "Channels to filter by")
	guildsFlag = flag.String("guilds", os.Getenv("DISCORD_GUILDS"), "Guilds to filter by (only non-search)")
	authorsFlag = flag.String("authors", os.Getenv("DISCORD_AUTHORS"), "Search result authors to filter by")
	botToken = flag.String("bottoken", os.Getenv("DISCORD_BOT_TOKEN"), "Bot token")
	userToken = flag.String("usertoken", os.Getenv("DISCORD_USER_TOKEN"), "User token")

	flag.Parse()

	filterChannels = parseIntSlice(channelsFlag, ",")
	filterGuilds = parseIntSlice(guildsFlag, ",")
	filterAuthors = parseIntSlice(authorsFlag, ",")

	// Let the user select a directory
	dir := selectDir(true)
	dir = formatDir(dir)
	fmt.Printf("Searching directory \"%s\"\n", dir)

	// Find all messages.csv files in said dir
	validFiles := getFileList(dir)
	validFilesAmt := len(validFiles)

	if *useSearch {
		fmt.Printf("Found %v search result JSONs\n", validFilesAmt)
		if validFilesAmt == 0 {
			fmt.Printf("Couldn't find any search result JSONs, did you set -dir to a directory containing .json files? Exiting...\n")
			return
		}
	} else {
		fmt.Printf("Found %v channel folders\n", validFilesAmt)
	}

	fmt.Printf("Filtering to %v channels\n", len(filterChannels))
	fmt.Printf("Filtering to %v guilds\n", len(filterGuilds))
	fmt.Printf("Filtering to %v authors\n", len(filterAuthors))

	if validFilesAmt == 0 {
		fmt.Printf("Couldn't find any %s files, maybe try another directory? "+
			"Make sure you are selecting the messages directory which contains c<number> folders, "+
			"or a c<number> folder itself. Exiting...\n", messagesCsv)
		return
	}

	if len(filterGuilds) > 0 {
		fmt.Printf("Warning: filtering to guilds doesn't work when using search results. Ensure you have filtered search results yourself.")
	}

	channels := extractMessageIDs(validFiles)
	if !confirmRun("mass delete") {
		fmt.Printf("Exiting...\n")
		return
	}

	deleteForAllChannels(channels)

	for {
		if retryMessages > 0 {
			fmt.Printf("Found %v channels with %v messages to retry deleting\n", len(retryChannels), retryMessages)
			if !confirmRun("retry deleting") {
				break
			}

			deleteForAllChannels(retryChannels)
		}
	}
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
func deleteForAllChannels(channels map[int64]Channel) {
	// Reset retry channels upon new run
	retryChannels = make(map[int64]Channel, 0)

	for _, channel := range channels {
		for _, message := range channel.Messages {
			deleteUrl := fmt.Sprintf("%s%v/messages/%v", baseUrl, channel.ID, message)
			err, retry := deleteChannelMessages(deleteUrl)

			// Add list to retry channels
			if retry {
				retryMessages++

				if retryChannel, ok := retryChannels[channel.ID]; ok && len(retryChannel.Messages) > 0 {
					retryChannel.Messages = append(retryChannel.Messages, message)
					retryChannels[channel.ID] = retryChannel
				} else {
					retryChannel := Channel{ID: channel.ID, Messages: []int64{message}}
					retryChannels[channel.ID] = retryChannel
				}
			}

			if err != nil {
				fmt.Printf("Error deleting channel messages: %v\n", err)
			}
		}
	}
}

// deleteChannelMessages will delete a message with the given url
func deleteChannelMessages(url string) (error, bool) {
	req, _ := http.NewRequest("DELETE", url, nil)
	if len(*botToken) > 0 {
		req.Header.Set("Auth", "Bot "+*botToken)
	} else {
		req.Header.Set("Authorization", *userToken)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Error making request: %v\n", err)
		return err, false
	}

	reqDump := make([]byte, 0)
	reqDump, err = httputil.DumpResponse(res, true)
	if err != nil {
		log.Printf("Error dumping response: %v\n")
		// return later
	}

	log.Printf("Result: %v\n%s\n(%v) %s\n", res.StatusCode, url, err, reqDump)

	if res.StatusCode == http.StatusTooManyRequests {
		remaining := parseStrUnsafe(res.Header.Get("X-RateLimit-Remaining"))
		reset := parseStrUnsafe(res.Header.Get("X-RateLimit-Reset"))

		now := time.Now()
		if len(res.Header.Get("Retry-After")) > 0 {
			remaining = 0
			reset = now.Unix() + parseStrUnsafe(res.Header.Get("Retry-After"))
		}

		if remaining == 0 {
			reset += retryAttempts
			wait := time.Duration(reset-now.Unix()) * time.Second
			retryAttempts++
			log.Printf("Waiting for %v seconds due to rate limit\n", wait)
			time.Sleep(wait)

			return err, true
		}
	} else if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNoContent && retryAttempts >= 15 {
		retryAttempts = 0
		log.Printf("Resetting retryAttempts back to 0 because %v and retryAttempts > 15\n", res.StatusCode)
	}

	return err, false
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

	data, err := os.ReadFile(jsonPath)
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
func extractMessageIDs(files []string) map[int64]Channel {
	channels := make(map[int64]Channel, 0)
	totalMessages := 0

	if *useSearch {
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				log.Printf("Skipping \"%s\" because: %v\n", file, err)
				continue
			}

			var results []SearchResult
			err = json.Unmarshal(data, &results)
			if err != nil {
				log.Printf("Skipping \"%s\" because: %v\n", file, err)
				continue
			}

			for _, result := range results {
				if ok := searchUrlRegex.MatchString(result.URL); ok && result.Content.Content != nil {
					var resultContent SearchResultContent
					err = json.Unmarshal([]byte(*result.Content.Content), &resultContent)
					if err != nil {
						log.Printf("Skipping \"%s\" because: (unmarshal) %v\n%s\n", result.URL, err, *result.Content.Content)
						continue
					}

					for _, messages := range resultContent.Messages {
						for _, message := range messages {
							if len(filterAuthors) > 0 && !contains(filterAuthors, message.Author.ID) {
								log.Printf("Skipping \"%v\" because filterAuthors contains \"%v\"", message.ID, message.Author.ID)
								continue
							}

							if len(filterChannels) > 0 && !contains(filterChannels, message.ChannelID) {
								log.Printf("Skipping \"%v\" because filterChannels contains \"%v\"", message.ID, message.ChannelID)
								continue
							}

							totalMessages++

							// Finally, we append or create a new channel if we have a valid parsed message and have passed filters
							if channel, ok := channels[message.ChannelID]; ok && len(channel.Messages) > 0 {
								channel.Messages = append(channel.Messages, message.ID)
								channels[message.ChannelID] = channel
							} else {
								channel = Channel{ID: message.ChannelID, Messages: []int64{message.ID}}
								channels[message.ChannelID] = channel
							}
						}
					}
				}
			}
		}
	} else {
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

			// Finally, create a new channel if we have a valid parsed message and have passed filters
			channels[channel.ID] = channel
		}
	}

	fmt.Printf("Found %v channels with %v messages\n", len(channels), totalMessages)
	return channels
}

// getFileList will look for all the messages.csv files that exist in dir, or a .json if we have useSearch enabled
func getFileList(dir string) []string {
	files := make([]string, 0)

	fileInfos, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}

	if *useSearch {
		for _, f := range fileInfos {
			if !f.IsDir() {
				// If we have a json file
				if strings.HasSuffix(f.Name(), ".json") {
					files = append(files, f.Name())
				}
			}
		}
	} else {
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
	if !confirmRun("selected directory") {
		fmt.Printf("Selected no, trying again.\n")
		return selectDir(false)
	}

	return dir
}

func confirmRun(s string) bool {
	fmt.Printf("Confirm %s? (Y/n): ", s)

	var confirm string
	fmt.Scanln(&confirm)

	// Default to yes
	if len(confirm) == 0 {
		return true
	}

	first := strings.ToLower(confirm[0:1])
	if first != "y" {
		return false
	}

	return true
}
