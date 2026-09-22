package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"neighborparking/internal/config"
	"neighborparking/internal/domain"
	"neighborparking/internal/platform"
	"neighborparking/internal/store"

	"golang.org/x/crypto/bcrypt"
)

const demoPassword = "DemoPass123!"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, _, err = st.UserByEmail(ctx, "owner@demo.local"); err == nil {
		fmt.Println("Demo data already exists. Sign in with owner@demo.local / " + demoPassword)
		return
	} else if !store.IsNotFound(err) {
		log.Fatal(err)
	}
	users := []domain.User{
		{ID: platform.NewID(), FullName: "Amina Rahman", Email: "owner@demo.local", Phone: "+8801700000001", VehicleName: "Toyota Corolla", LicensePlate: "DHAKA-12-3456"},
		{ID: platform.NewID(), FullName: "Farhan Ahmed", Email: "admin@demo.local", Phone: "+8801700000002", VehicleName: "Honda Civic", LicensePlate: "DHAKA-22-4821"},
		{ID: platform.NewID(), FullName: "Nusrat Jahan", Email: "member@demo.local", Phone: "+8801700000003", VehicleName: "Toyota Axio", LicensePlate: "DHAKA-15-7364"},
		{ID: platform.NewID(), FullName: "Rafi Islam", Email: "pending@demo.local", Phone: "+8801700000004", VehicleName: "Nissan X-Trail", LicensePlate: "DHAKA-31-1092"},
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(demoPassword), bcrypt.DefaultCost)
	for _, u := range users {
		if err = st.CreateUser(ctx, u, string(hash)); err != nil {
			log.Fatal(err)
		}
	}
	c := domain.Community{ID: platform.NewID(), Name: "Lakeview Residency", Code: "LAKE2026", Description: "A peaceful shared parking court for Lakeview residents.", Address: "Road 12, Dhanmondi, Dhaka", GridRows: 7, GridCols: 9}
	if err = st.CreateCommunity(ctx, c, users[0].ID); err != nil {
		log.Fatal(err)
	}
	for _, u := range users[1:] {
		if err = st.RequestJoin(ctx, c.ID, u.ID); err != nil {
			log.Fatal(err)
		}
	}
	requests, err := st.JoinRequests(ctx, c.ID)
	if err != nil {
		log.Fatal(err)
	}
	for _, request := range requests {
		if request.UserID == users[3].ID {
			continue
		}
		if err = st.DecideJoin(ctx, c.ID, request.ID, users[0].ID, domain.StatusApproved); err != nil {
			log.Fatal(err)
		}
		if request.UserID == users[1].ID {
			if err = st.ChangeRole(ctx, c.ID, request.ID, domain.RoleAdmin); err != nil {
				log.Fatal(err)
			}
		}
	}
	cells := demoCells()
	if _, err = st.SaveLayout(ctx, c.ID, domain.LayoutInput{Rows: 7, Cols: 9, LayoutVersion: 1, Cells: cells}); err != nil {
		log.Fatal(err)
	}
	parkingMap, err := st.Map(ctx, c.ID, users[0].ID)
	if err != nil {
		log.Fatal(err)
	}
	for _, cell := range parkingMap.Cells {
		if cell.Type == "PARKING" && cell.Label == "A-02" {
			if _, err = st.CheckIn(ctx, c.ID, cell.SlotID, users[2].ID, users[2].ID, "SELF"); err != nil {
				log.Fatal(err)
			}
			break
		}
	}
	fmt.Println("Demo data created.")
	fmt.Println("Owner:   owner@demo.local / " + demoPassword)
	fmt.Println("Admin:   admin@demo.local / " + demoPassword)
	fmt.Println("Member:  member@demo.local / " + demoPassword)
	fmt.Println("Pending: pending@demo.local / " + demoPassword)
}

func demoCells() []domain.MapCell {
	return []domain.MapCell{
		{Row: 0, Col: 0, Type: "ENTRY_GATE", Label: "Entry"}, {Row: 0, Col: 1, Type: "ROAD"}, {Row: 0, Col: 2, Type: "ROAD"}, {Row: 0, Col: 3, Type: "ROAD"}, {Row: 0, Col: 4, Type: "ROAD"}, {Row: 0, Col: 5, Type: "ROAD"}, {Row: 0, Col: 6, Type: "ROAD"}, {Row: 0, Col: 7, Type: "ROAD"}, {Row: 0, Col: 8, Type: "EXIT_GATE", Label: "Exit"},
		{Row: 1, Col: 1, Type: "PARKING", Label: "A-01"}, {Row: 1, Col: 2, Type: "PARKING", Label: "A-02"}, {Row: 1, Col: 3, Type: "PARKING", Label: "A-03"}, {Row: 1, Col: 5, Type: "PARKING", Label: "A-04"}, {Row: 1, Col: 6, Type: "PARKING", Label: "A-05"}, {Row: 1, Col: 7, Type: "PARKING", Label: "A-06"},
		{Row: 2, Col: 0, Type: "WALL"}, {Row: 2, Col: 1, Type: "ROAD"}, {Row: 2, Col: 2, Type: "ROAD"}, {Row: 2, Col: 3, Type: "ROAD"}, {Row: 2, Col: 4, Type: "PILLAR"}, {Row: 2, Col: 5, Type: "ROAD"}, {Row: 2, Col: 6, Type: "ROAD"}, {Row: 2, Col: 7, Type: "ROAD"}, {Row: 2, Col: 8, Type: "WALL"},
		{Row: 3, Col: 1, Type: "PARKING", Label: "B-01"}, {Row: 3, Col: 2, Type: "PARKING", Label: "B-02"}, {Row: 3, Col: 3, Type: "PARKING", Label: "B-03"}, {Row: 3, Col: 5, Type: "PARKING", Label: "B-04"}, {Row: 3, Col: 6, Type: "PARKING", Label: "B-05"}, {Row: 3, Col: 7, Type: "PARKING", Label: "B-06"},
		{Row: 4, Col: 1, Type: "ROAD"}, {Row: 4, Col: 2, Type: "ROAD"}, {Row: 4, Col: 3, Type: "ROAD"}, {Row: 4, Col: 4, Type: "ROAD"}, {Row: 4, Col: 5, Type: "ROAD"}, {Row: 4, Col: 6, Type: "ROAD"}, {Row: 4, Col: 7, Type: "ROAD"},
		{Row: 5, Col: 1, Type: "PARKING", Label: "C-01"}, {Row: 5, Col: 2, Type: "PARKING", Label: "C-02"}, {Row: 5, Col: 3, Type: "PARKING", Label: "C-03"}, {Row: 5, Col: 5, Type: "PARKING", Label: "C-04"}, {Row: 5, Col: 6, Type: "PARKING", Label: "C-05"}, {Row: 5, Col: 7, Type: "NO_PARKING", Label: "Reserved"},
	}
}
