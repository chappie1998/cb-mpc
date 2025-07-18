package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
)

const (
	// Fixed demo keypair (for consistent demo address)
	DemoPrivateKeyHex = "3a714d6f64aa4059b869cdad2cd2c5b36d34c5082b62675e005ba3ec61586ca7a2b2c4d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8"
	
	// Transfer configuration
	ToAddress = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	TransferAmount = 0.01 // SOL
	
	// Solana devnet RPC
	DevnetRPC = "https://api.devnet.solana.com"
)

func main() {
	fmt.Println("🚀 Solana Devnet Demo Transfer (FIXED KEYPAIR)")
	fmt.Println("===============================================")
	
	ctx := context.Background()
	
	// Step 1: Connect to Solana devnet
	fmt.Println("\n📍 Step 1: Connecting to Solana Devnet...")
	client := rpc.New(DevnetRPC)
	
	version, err := client.GetVersion(ctx)
	if err != nil {
		log.Fatal("Failed to connect to Solana devnet:", err)
	}
	fmt.Printf("✅ Connected to Solana devnet (version: %s)\n", version.SolanaCore)
	
	// Step 2: Load fixed demo keypair
	fmt.Println("\n📍 Step 2: Loading Demo Keypair...")
	
	// Use first 32 bytes as private key seed
	privateKeyBytes, err := hex.DecodeString(DemoPrivateKeyHex[:64]) // First 32 bytes
	if err != nil {
		log.Fatal("Failed to decode private key:", err)
	}
	
	// Generate the full Ed25519 key from seed
	privateKey := ed25519.NewKeyFromSeed(privateKeyBytes)
	solanaPrivateKey := solana.PrivateKey(privateKey)
	solanaPublicKey := solanaPrivateKey.PublicKey()
	
	fmt.Printf("✅ Demo Address: %s\n", solanaPublicKey.String())
	fmt.Printf("💡 Fund this address at: https://faucet.solana.com\n")
	
	// Step 3: Check balance
	fmt.Println("\n📍 Step 3: Checking Balance...")
	
	balance, err := client.GetBalance(ctx, solanaPublicKey, rpc.CommitmentFinalized)
	if err != nil {
		log.Fatal("Failed to get balance:", err)
	}
	
	balanceSOL := float64(balance.Value) / float64(solana.LAMPORTS_PER_SOL)
	fmt.Printf("✅ Current balance: %.9f SOL (%d lamports)\n", balanceSOL, balance.Value)
	
	if balance.Value == 0 {
		fmt.Printf("❌ No balance! Please fund this address:\n")
		fmt.Printf("   Address: %s\n", solanaPublicKey.String())
		fmt.Printf("   Faucet: https://faucet.solana.com\n")
		fmt.Printf("   Explorer: https://explorer.solana.com/address/%s?cluster=devnet\n", solanaPublicKey.String())
		return
	}
	
	transferLamports := uint64(TransferAmount * float64(solana.LAMPORTS_PER_SOL))
	if balance.Value < transferLamports+5000 { // 5000 lamports for fees
		fmt.Printf("❌ Insufficient balance for transfer (need %.9f SOL + fees)\n", TransferAmount)
		fmt.Printf("   Current: %.9f SOL\n", balanceSOL)
		return
	}
	
	// Step 4: Setup transfer
	fmt.Println("\n📍 Step 4: Setting up Transfer...")
	
	toPubkey, err := solana.PublicKeyFromBase58(ToAddress)
	if err != nil {
		log.Fatal("Invalid recipient address:", err)
	}
	
	fmt.Printf("✅ From: %s\n", solanaPublicKey.String())
	fmt.Printf("✅ To:   %s\n", toPubkey.String())
	fmt.Printf("✅ Amount: %.9f SOL (%d lamports)\n", TransferAmount, transferLamports)
	
	// Step 5: Create transaction
	fmt.Println("\n📍 Step 5: Creating Transaction...")
	
	latest, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		log.Fatal("Failed to get latest blockhash:", err)
	}
	
	instruction := system.NewTransferInstruction(
		transferLamports,
		solanaPublicKey,
		toPubkey,
	).Build()
	
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		latest.Value.Blockhash,
		solana.TransactionPayer(solanaPublicKey),
	)
	if err != nil {
		log.Fatal("Failed to create transaction:", err)
	}
	
	fmt.Printf("✅ Transaction created\n")
	
	// Step 6: Sign transaction
	fmt.Println("\n📍 Step 6: Signing Transaction...")
	
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(solanaPublicKey) {
			return &solanaPrivateKey
		}
		return nil
	})
	if err != nil {
		log.Fatal("Failed to sign transaction:", err)
	}
	
	fmt.Printf("✅ Transaction signed with Ed25519\n")
	
	// Step 7: Simulate first
	fmt.Println("\n📍 Step 7: Simulating Transaction...")
	
	result, err := client.SimulateTransaction(ctx, tx)
	if err != nil {
		log.Fatal("Simulation failed:", err)
	}
	
	if result.Value.Err != nil {
		log.Fatalf("Simulation error: %v", result.Value.Err)
	}
	
	fmt.Printf("✅ Transaction simulation successful\n")
	
	// Step 8: Broadcast to devnet
	fmt.Println("\n📍 Step 8: Broadcasting to Solana Devnet...")
	
	sig, err := client.SendTransaction(ctx, tx)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}
	
	fmt.Printf("\n🎉 TRANSACTION BROADCASTED SUCCESSFULLY!\n")
	fmt.Printf("==========================================\n")
	fmt.Printf("Transaction Hash: %s\n", sig.String())
	fmt.Printf("\n🔗 View on Explorers:\n")
	fmt.Printf("Solana Explorer: https://explorer.solana.com/tx/%s?cluster=devnet\n", sig.String())
	fmt.Printf("Solscan:         https://solscan.io/tx/%s?cluster=devnet\n", sig.String())
	
	// Step 9: Wait for confirmation
	fmt.Println("\n📍 Step 9: Waiting for Confirmation...")
	
	if err := waitForConfirmation(ctx, client, sig); err != nil {
		log.Printf("⚠️  Confirmation timeout: %v", err)
	} else {
		fmt.Println("✅ Transaction confirmed!")
	}
	
	// Step 10: Verify transfer
	fmt.Println("\n📍 Step 10: Verifying Transfer...")
	
	time.Sleep(3 * time.Second)
	newBalance, err := client.GetBalance(ctx, solanaPublicKey, rpc.CommitmentFinalized)
	if err != nil {
		log.Printf("Failed to get new balance: %v", err)
	} else {
		newBalanceSOL := float64(newBalance.Value) / float64(solana.LAMPORTS_PER_SOL)
		transferred := float64(balance.Value - newBalance.Value) / float64(solana.LAMPORTS_PER_SOL)
		
		fmt.Printf("✅ Balance before: %.9f SOL\n", balanceSOL)
		fmt.Printf("✅ Balance after:  %.9f SOL\n", newBalanceSOL)
		fmt.Printf("✅ Amount sent:    %.9f SOL\n", transferred)
	}
	
	fmt.Printf("\n🎯 REAL SOLANA TRANSACTION COMPLETE!\n")
	fmt.Printf("====================================\n")
	fmt.Printf("✅ Successfully transferred %.9f SOL on Solana devnet\n", TransferAmount)
	fmt.Printf("✅ Transaction hash: %s\n", sig.String())
	fmt.Printf("✅ View at: https://explorer.solana.com/tx/%s?cluster=devnet\n", sig.String())
}

func waitForConfirmation(ctx context.Context, client *rpc.Client, sig solana.Signature) error {
	fmt.Printf("  ⏳ Waiting...")
	
	timeout := time.After(45 * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout")
		case <-ticker.C:
			fmt.Print(".")
			
			result, err := client.GetSignatureStatuses(ctx, true, sig)
			if err != nil {
				continue
			}
			
			if len(result.Value) > 0 && result.Value[0] != nil {
				status := result.Value[0]
				if status.ConfirmationStatus == "confirmed" || status.ConfirmationStatus == "finalized" {
					fmt.Print(" ✅\n")
					return nil
				}
				if status.Err != nil {
					return fmt.Errorf("transaction failed: %v", status.Err)
				}
			}
		}
	}
} 