# discord-mass-delete

A quick tool to retroactively delete all messages (or filter to specific channels / guilds), extracted from your Discord data backup.

## Usage

```bash
git clone git@github.com:l1ving/discord-mass-delete.git
cd discord-mass-delete
make
./mass-delete -h # Run the program with help arguments
```

## Running

1. Download your Discord data backup. You can get this by going to Discord Settings > Privacy & safety > Request all of my data
2. Extract the data somewhere. Doesn't matter.
3. Follow the above [usage](#Usage) instructions and run the program from anywhere.
4. Follow the interactive instructions. You can use the `-dir $DIR -dirconfirm` args with `DIR` set to a path to skip the prompts.
The `-bottoken` arg is the only required argument. `-channels` is a comma separated list of channel IDs.

Example:
```bash
./mass-delete -dirconfirm -dir "$DIR" -channels "$FILTER" -bottoken "$TOKEN"
```

#### Filtering

There are two possible filter flags: `-channels` and `-guilds`. Both are comma separated lists of IDs.

The channels filter takes precedence over guilds. 
For example, if you specify a channels filter and a channel is not found in the list, it will be skipped.
If you specify a guilds filter and a channel is not inside one of the specified guilds, it will also be skipped, regardless of the channel setting.

It is not necessary to use both options if you want to select specific channels inside a guild, as supplying the channels is good enough.

#### Using a user account instead of a bot account

This program respects Discord's rate limits, so while it *is* against Discord TOS, you can't really get banned for using it (use user tokens at your own risk, I am not liable).

The first step is required for it to work. 
Everything afterwards is only something you should do if you are paranoid about your account getting banned.

- Edit `main.go#deleteChannelMessages()` to not have `Bot ` inside the `Auth` header.
- Add the regular headers that you would get from deleting a message.
  - Press <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>I</kbd>
  - Go to the Network tab
  - Delete a message
  - Hit the last request (usually status 204, make sure the Request Method is `DELETE`)
  - Go to Headers
  - Scroll down to Request Headers
  - Add a line that looks like `req.Header.Set("header name", "request value")`, for each header
  - Now you can follow the [Usage](#Usage) instructions. Use your user token with the `-bottoken` arg.
