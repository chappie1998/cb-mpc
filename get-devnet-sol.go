package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

const (
	// Use the latest wallet address
	MyWalletAddress = "2Y1Bw3vbdATKey1pDZaMAPXmBFjgswAsREKnsJb8omTZ"
	DevnetRPC = "https://api.devnet.solana.com"
)

type FaucetRequest struct {
	Pubkey string `json:"pubkey"`
}

func main() {
	fmt.Println("💰 Requesting Devnet SOL for MPC Wallet")
	fmt.Println("=====================================")
	
	ctx := context.Background()
	
	// Step 1: Connect to devnet
	fmt.Println("\n📍 Step 1: Connecting to Solana Devnet...")
	client := rpc.New(DevnetRPC)
	
	pubkey, err := solana.PublicKeyFromBase58(MyWalletAddress)
	if err != nil {
		log.Fatal("Invalid wallet address:", err)
	}
	
	fmt.Printf("✅ Wallet address: %s\n", pubkey.String())
	
	// Step 2: Check current balance
	fmt.Println("\n📍 Step 2: Checking Current Balance...")
	
	balance, err := client.GetBalance(ctx, pubkey, rpc.CommitmentFinalized)
	if err != nil {
		log.Fatal("Failed to get balance:", err)
	}
	
	balanceSOL := float64(balance.Value) / 1e9
	fmt.Printf("✅ Current balance: %.9f SOL (%d lamports)\n", balanceSOL, balance.Value)
	
	// Step 3: Request SOL from faucet
	fmt.Println("\n📍 Step 3: Requesting SOL from Devnet Faucet...")
	
	if err := requestFromFaucet(pubkey.String()); err != nil {
		log.Printf("❌ Faucet request failed: %v", err)
		fmt.Println("\n💡 Manual alternatives:")
		fmt.Printf("1. Visit: https://faucet.solana.com\n")
		fmt.Printf("2. Enter address: %s\n", pubkey.String())
		fmt.Printf("3. Click 'Request SOL'\n")
		return
	}
	
	fmt.Println("✅ Faucet request successful!")
	
	// Step 4: Wait and check new balance
	fmt.Println("\n📍 Step 4: Waiting for SOL to arrive...")
	
	for i := 0; i < 12; i++ { // Wait up to 60 seconds
		time.Sleep(5 * time.Second)
		fmt.Print(".")
		
		newBalance, err := client.GetBalance(ctx, pubkey, rpc.CommitmentFinalized)
		if err != nil {
			continue
		}
		
		if newBalance.Value > balance.Value {
			newBalanceSOL := float64(newBalance.Value) / 1e9
			fmt.Printf("\n✅ Success! New balance: %.9f SOL (%d lamports)\n", newBalanceSOL, newBalance.Value)
			
			received := float64(newBalance.Value - balance.Value) / 1e9
			fmt.Printf("✅ Received: %.9f SOL\n", received)
			
			fmt.Println("\n🚀 Ready to run the transfer script!")
			fmt.Println("Run: go run solana-devnet-transfer.go")
			return
		}
	}
	
	fmt.Println("\n⏳ SOL not received yet. Check manually:")
	fmt.Printf("https://explorer.solana.com/address/%s?cluster=devnet\n", pubkey.String())
}

func requestFromFaucet(address string) error {
	// Try the official Solana faucet API
	faucetURL := "https://faucet.solana.com/api/v1/airdrop"
	
	requestBody := FaucetRequest{
		Pubkey: address,
	}
	
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}
	
	req, err := http.NewRequest("POST", faucetURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("faucet returned status %d: %s", resp.StatusCode, string(body))
	}
	
	fmt.Printf("✅ Faucet response: %s\n", string(body))
	return nil
} 