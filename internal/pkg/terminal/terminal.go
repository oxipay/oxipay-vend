package terminal

import (
	"database/sql"
	"errors"
	"log"
)

// Terminal terminal mapping
type Terminal struct {
	FxlRegisterID       string // Oxipay registerid
	FxlSellerID         string
	FxlDeviceSigningKey string
	Origin              string
	VendRegisterID      string
}

//Db connection to the database
var Db *sql.DB

//NewTerminal returns a Pointer to a terminal
func NewTerminal(key string, deviceID string, merchantID string, origin string, registerID string) *Terminal {
	return &Terminal{
		FxlDeviceSigningKey: key,
		FxlRegisterID:       deviceID,
		FxlSellerID:         merchantID, // Oxipay Merchant No
		Origin:              origin,     // Vend Website
		VendRegisterID:      registerID, // Vend Register ID
	}
}

//Save will save the terminal to the database
func (t Terminal) Save(user string) (bool, error) {

	if Db == nil {
		return false, errors.New("I have no database connection")
	}

	query := `INSERT INTO 
		oxipay_vend_map  
		(
			fxl_register_id,
			fxl_seller_id,
			fxl_device_signing_key,
			origin_domain, 
			vend_register_id,
			created_by
		) VALUES (?, ?, ?, ?, ?, ?) `

	stmt, err := Db.Prepare(query)

	if err != nil {
		return false, err
	}

	defer stmt.Close()

	_, err = stmt.Exec(
		newNullString(t.FxlRegisterID),
		newNullString(t.FxlSellerID),
		newNullString(t.FxlDeviceSigningKey),
		newNullString(t.Origin),
		newNullString(t.VendRegisterID),
		newNullString(user),
	)

	if err != nil {
		return false, err
	}

	return true, nil
}

// GetRegisteredTerminal will return a registered terminal for the the domain & vendregister_id combo
func GetRegisteredTerminal(originDomain string, vendRegisterID string) (*Terminal, error) {

	if Db == nil {
		return nil, errors.New("I have no database connection")
	}

	sql := `SELECT 
			 fxl_register_id, 
			 fxl_seller_id,
			 fxl_device_signing_key, 
			 origin_domain,
			 vend_register_id
			FROM 
				oxipay_vend_map 
			WHERE 
				origin_domain = ? 
			AND
				vend_register_id = ? 
			AND 1=1`

	rows, err := Db.Query(sql, originDomain, vendRegisterID)

	if err != nil {
		log.Fatal(err)
	}

	var terminal = new(Terminal)
	noRows := 0

	for rows.Next() {
		noRows++
		var err = rows.Scan(
			&terminal.FxlRegisterID,
			&terminal.FxlSellerID,
			&terminal.FxlDeviceSigningKey,
			&terminal.Origin,
			&terminal.VendRegisterID,
		)
		if err != nil {
			log.Fatal(err)
		}
	}

	if noRows < 1 {
		return nil, errors.New("Unable to find a matching terminal ")
	}

	return terminal, nil
}

func newNullString(s string) sql.NullString {
	if len(s) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}
