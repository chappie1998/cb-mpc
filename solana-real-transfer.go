package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
)

const (
	// Transfer configuration
	ToAddress = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM" // Random devnet address
	TransferAmount = 0.01 // SOL
	
	// Solana devnet RPC
	DevnetRPC = "https://api.devnet.solana.com"
)

func main() {
	fmt.Println("🚀 Real Solana Devnet Transfer (WORKING DEMO)")
	fmt.Println("==============================================")
	
	ctx := context.Background()
	
	// Step 1: Connect to Solana devnet
	fmt.Println("\n📍 Step 1: Connecting to Solana Devnet...")
	client := rpc.New(DevnetRPC)
	
	// Test connection
	version, err := client.GetVersion(ctx)
	if err != nil {
		log.Fatal("Failed to connect to Solana devnet:", err)
	}
	fmt.Printf("✅ Connected to Solana devnet (version: %s)\n", version.SolanaCore)
	
	// Step 2: Generate a new keypair for this demo
	fmt.Println("\n📍 Step 2: Generating New Keypair...")
	
	// Generate Ed25519 keypair
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal("Failed to generate keypair:", err)
	}
	
	// Convert to Solana types
	solanaPrivateKey := solana.PrivateKey(privateKey)
	solanaPublicKey := solanaPrivateKey.PublicKey()
	
	fmt.Printf("✅ Generated keypair\n")
	fmt.Printf("✅ Public Key: %s\n", solanaPublicKey.String())
	fmt.Printf("✅ Private Key: %s\n", hex.EncodeToString(privateKey))
	
	// Step 3: Request devnet SOL for this address
	fmt.Println("\n📍 Step 3: Requesting Devnet SOL...")
	
	// Request airdrop
	airdropSig, err := client.RequestAirdrop(ctx, solanaPublicKey, 2*solana.LAMPORTS_PER_SOL, rpc.CommitmentFinalized)
	if err != nil {
		fmt.Printf("⚠️  Airdrop failed: %v\n", err)
		fmt.Printf("💡 Manual option: Visit https://faucet.solana.com\n")
		fmt.Printf("💡 Address: %s\n", solanaPublicKey.String())
		
		// Try to continue anyway - maybe there's already a balance
	} else {
		fmt.Printf("✅ Airdrop requested: %s\n", airdropSig.String())
	}
	
	// Wait for airdrop to complete
	fmt.Println("⏳ Waiting for airdrop...")
	time.Sleep(10 * time.Second)
	
	// Step 4: Check balance
	fmt.Println("\n📍 Step 4: Checking Balance...")
	
	balance, err := client.GetBalance(ctx, solanaPublicKey, rpc.CommitmentFinalized)
	if err != nil {
		log.Fatal("Failed to get balance:", err)
	}
	
	balanceSOL := float64(balance.Value) / float64(solana.LAMPORTS_PER_SOL)
	fmt.Printf("✅ Current balance: %.9f SOL (%d lamports)\n", balanceSOL, balance.Value)
	
	if balance.Value == 0 {
		log.Fatal("❌ No balance! Please get SOL from https://faucet.solana.com for address:", solanaPublicKey.String())
	}
	
	transferLamports := uint64(TransferAmount * float64(solana.LAMPORTS_PER_SOL))
	if balance.Value < transferLamports+5000 { // 5000 lamports for fees
		log.Fatalf("❌ Insufficient balance for transfer (need %.9f SOL + fees)", TransferAmount)
	}
	
	// Step 5: Setup recipient
	fmt.Println("\n📍 Step 5: Setting up Transfer...")
	
	toPubkey, err := solana.PublicKeyFromBase58(ToAddress)
	if err != nil {
		log.Fatal("Invalid recipient address:", err)
	}
	
	fmt.Printf("✅ From: %s\n", solanaPublicKey.String())
	fmt.Printf("✅ To:   %s\n", toPubkey.String())
	fmt.Printf("✅ Amount: %.9f SOL\n", TransferAmount)
	
	// Step 6: Create transaction
	fmt.Println("\n📍 Step 6: Creating Transaction...")
	
	// Get latest blockhash
	latest, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		log.Fatal("Failed to get latest blockhash:", err)
	}
	
	// Create transfer instruction
	instruction := system.NewTransferInstruction(
		transferLamports,
		solanaPublicKey,
		toPubkey,
	).Build()
	
	// Create transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		latest.Value.Blockhash,
		solana.TransactionPayer(solanaPublicKey),
	)
	if err != nil {
		log.Fatal("Failed to create transaction:", err)
	}
	
	fmt.Printf("✅ Transaction created\n")
	
	// Step 7: Sign transaction
	fmt.Println("\n📍 Step 7: Signing Transaction...")
	
	// Sign with our private key
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(solanaPublicKey) {
			return &solanaPrivateKey
		}
		return nil
	})
	if err != nil {
		log.Fatal("Failed to sign transaction:", err)
	}
	
	fmt.Printf("✅ Transaction signed\n")
	
	// Step 8: Simulate transaction first
	fmt.Println("\n📍 Step 8: Simulating Transaction...")
	
	result, err := client.SimulateTransaction(ctx, tx)
	if err != nil {
		log.Fatal("Simulation failed:", err)
	}
	
	if result.Value.Err != nil {
		log.Fatalf("Simulation error: %v", result.Value.Err)
	}
	
	fmt.Printf("✅ Transaction simulation successful\n")
	
	// Step 9: Send transaction to devnet
	fmt.Println("\n📍 Step 9: Broadcasting to Solana Devnet...")
	
	sig, err := client.SendTransaction(ctx, tx)
	if err != nil {
		log.Fatal("Failed to send transaction:", err)
	}
	
	fmt.Printf("🎉 TRANSACTION SENT SUCCESSFULLY!\n")
	fmt.Printf("=======================================\n")
	fmt.Printf("Transaction Signature: %s\n", sig.String())
	fmt.Printf("Solana Explorer: https://explorer.solana.com/tx/%s?cluster=devnet\n", sig.String())
	fmt.Printf("Solscan: https://solscan.io/tx/%s?cluster=devnet\n", sig.String())
	
	// Step 10: Wait for confirmation
	fmt.Println("\n📍 Step 10: Waiting for Confirmation...")
	
	if err := waitForConfirmation(ctx, client, sig); err != nil {
		log.Printf("⚠️  Error waiting for confirmation: %v", err)
	} else {
		fmt.Println("✅ Transaction confirmed on Solana devnet!")
	}
	
	// Step 11: Verify balance change
	fmt.Println("\n📍 Step 11: Verifying Transfer...")
	
	time.Sleep(2 * time.Second) // Wait for balance update
	newBalance, err := client.GetBalance(ctx, solanaPublicKey, rpc.CommitmentFinalized)
	if err != nil {
		log.Printf("Failed to get new balance: %v", err)
	} else {
		newBalanceSOL := float64(newBalance.Value) / float64(solana.LAMPORTS_PER_SOL)
		fmt.Printf("✅ New balance: %.9f SOL (%d lamports)\n", newBalanceSOL, newBalance.Value)
		
		transferred := float64(balance.Value - newBalance.Value) / float64(solana.LAMPORTS_PER_SOL)
		fmt.Printf("✅ Successfully transferred: %.9f SOL\n", transferred)
	}
	
	fmt.Printf("\n🎯 REAL TRANSACTION COMPLETE!\n")
	fmt.Printf("Check on explorer: https://explorer.solana.com/tx/%s?cluster=devnet\n", sig.String())
}

func waitForConfirmation(ctx context.Context, client *rpc.Client, sig solana.Signature) error {
	fmt.Printf("  ⏳ Waiting for confirmation...")
	
	timeout := time.After(60 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for confirmation")
		case <-ticker.C:
			fmt.Print(".")
			
			result, err := client.GetSignatureStatuses(ctx, true, sig)
			if err != nil {
				continue
			}
			
			if len(result.Value) > 0 && result.Value[0] != nil {
				status := result.Value[0]
				if status.ConfirmationStatus == "confirmed" || status.ConfirmationStatus == "finalized" {
					fmt.Println(" ✅")
					return nil
				}
				if status.Err != nil {
					return fmt.Errorf("transaction failed: %v", status.Err)
				}
			}
		}
	}
} 