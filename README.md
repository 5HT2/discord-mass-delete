# discord-mass-delete

A quick tool to retroactively delete all messages (or filter to specific channels), extracted from your Discord data backup.

## Usage

These instructions are for building to Go project. 

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
