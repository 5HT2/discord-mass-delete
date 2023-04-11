discord-mass-delete: clean
	go get -u "github.com/joho/godotenv"
	go build -o mass-delete
clean:
	rm -f mass-delete
