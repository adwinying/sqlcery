package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/demo/seed.go <database>")
		os.Exit(2)
	}

	database, err := sql.Open("sqlite", os.Args[1])
	if err != nil {
		fail(err)
	}
	defer database.Close()

	_, err = database.Exec(`
		CREATE TABLE orders (
			id INTEGER PRIMARY KEY,
			customer TEXT NOT NULL,
			status TEXT NOT NULL,
			total_cents INTEGER NOT NULL
		);

		INSERT INTO orders (customer, status, total_cents) VALUES
			('Aperture Labs', 'processing', 24800),
			('Northstar Goods', 'shipped', 7950),
			('Juniper Studio', 'pending', 16350),
			('Kite & Key', 'delivered', 42100),
			('Sable Works', 'processing', 10900);
	`)
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "seed demo database: %v\n", err)
	os.Exit(1)
}
