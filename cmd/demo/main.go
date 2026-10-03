// Command demo simulates one STC bill payment (following the flowchart) plus
// general server logging, so you can see the folders and files produced.
package main

import (
	"log"

	"github.com/amahdi-q/app-loger/applog"
)

func main() {
	lg, err := applog.New(applog.Config{
		RootDir:      "./PIEServer",               // where to save
		DirLayout:    "{folder}/{yyyy}/{mm}/{dd}", // folder structure
		FileExt:      ".txt",
		ErrorsFolder: "Errors", // every warning/error is also copied here
		ErrorsFile:   "errors",
	})
	if err != nil {
		log.Fatal(err)
	}

	// General server log: PIEServer/System/yyyy/mm/dd/server.txt
	sys, _ := lg.Stream("System", "server")
	sys.Info("PIEServer started on :8080")
	sys.Warn("Batelco endpoint slow to respond (2.4s)")

	// ---- Request 1: Transaction initialisation (bill inquiry) ----
	tx, err := lg.Session("STC", "100245") // PIEServer/STC/yyyy/mm/dd/100245.txt
	if err != nil {
		log.Fatal(err)
	}
	tx.Step("Bill inquiry request")
	tx.Info("Bill payment request received from kiosk K-012")
	tx.Field("Mobile number", "33333333")
	tx.Info("Request decoded successfully")
	tx.Payload("SOAP request to STC", "<soap:Envelope>\n  <GetBill msisdn=\"33333333\"/>\n</soap:Envelope>")
	tx.Payload("SOAP response from STC", "<soap:Envelope>\n  <Bill due=\"50.000\"/>\n</soap:Envelope>")
	tx.Field("Bill amount due", "50.000 BHD")
	tx.Info("Bill details sent to kiosk")

	// ---- Request 2: Customer entered amount -> authorize ----
	tx, _ = lg.Session("STC", "100245") // same ID -> same file
	tx.Step("Authorize transaction")
	tx.Field("Amount to pay", 50)
	tx.Info("Authorize request sent to STC")
	tx.Info("Authorization approved")

	// ---- Request 3: Cash inserted -> verify & confirm ----
	tx, _ = lg.Session("STC", "100245")
	tx.Step("Verify payment and confirm")
	tx.Field("Amount inserted", 50)
	tx.Info("Payment verified")
	tx.Info("Confirm request sent to STC")
	tx.Field("STC reference", "STC-88412")
	tx.Info("Confirmation sent to kiosk")
	tx.End("success")

	// A failed transaction: the error also lands in Errors/.../errors.txt
	bad, _ := lg.Session("Zain", "100246")
	bad.Step("Bill inquiry request")
	bad.Field("Mobile number", "36666666")
	bad.Error("Zain SOAP call failed: connection timeout after 30s")
	bad.End("failed")
}
