package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/coinbase/cb-mpc/demos-go/cb-mpc-go/api/curve"
	"github.com/coinbase/cb-mpc/demos-go/cb-mpc-go/api/mpc"
	"github.com/coinbase/cb-mpc/demos-go/cb-mpc-go/api/transport/mocknet"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
)

const (
	// Configuration from wallet generator
	PINSalt = "solana-cb-mpc-salt-2024"
	PBKDFIterations = 100000
	TestPIN = "123456"
	
	// Latest wallet shares - UPDATE THESE from latest wallet generator run
	S1Share = "C38CAQL/gAABCgAA/8//gAAFIUAF83fys98VwMWbEzX1WGeETVZocYWJZElXGqdRtSCA1CIEP76k1u2BZw/MmMe1OVo82mKrWW/jWbmyWAFBElHCeoQleQMAA2ttcwQ/RclMbbmtJcmblf7ZdR5QM8gFhWqJn9koRCXyUzx6ct8AA3BpbgQ/lQdhUDG/+AGcIjoGxPER16hutJgvXd5jMmCkZMgOFesABnNlcnZlcgQ/rvy6FBdJYJML+EunNqUxFs//bhk4Tc4OU3N1EeZDnEMCBD8IAAZzZXJ2ZXI="
	MyWalletAddress = "2Y1Bw3vbdATKey1pDZaMAPXmBFjgswAsREKnsJb8omTZ"
	
	// Transfer configuration
	ToAddress = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM" // Random devnet address
	TransferAmount = 0.01 // SOL (10,000,000 lamports)
	
	// Solana devnet RPC
	DevnetRPC = "https://api.devnet.solana.com"
)

type SolanaTransfer struct {
	FromAddress   solana.PublicKey
	ToAddress     solana.PublicKey
	Amount        uint64 // lamports
	Transaction   *solana.Transaction
	MPCSignature  []byte
}

func main() {
	fmt.Println("🚀 Solana Devnet MPC Transfer (REAL TRANSACTION)")
	fmt.Println("===============================================")
	
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
	
	// Step 2: Setup wallet addresses
	fmt.Println("\n📍 Step 2: Setting up Wallet Addresses...")
	
	fromPubkey, err := solana.PublicKeyFromBase58(MyWalletAddress)
	if err != nil {
		log.Fatal("Invalid from address:", err)
	}
	
	toPubkey, err := solana.PublicKeyFromBase58(ToAddress)
	if err != nil {
		log.Fatal("Invalid to address:", err)
	}
	
	fmt.Printf("✅ From: %s\n", fromPubkey.String())
	fmt.Printf("✅ To:   %s\n", toPubkey.String())
	
	// Step 3: Check balance
	fmt.Println("\n📍 Step 3: Checking Account Balance...")
	
	balance, err := client.GetBalance(ctx, fromPubkey, rpc.CommitmentFinalized)
	if err != nil {
		log.Fatal("Failed to get balance:", err)
	}
	
	balanceSOL := float64(balance.Value) / 1e9
	fmt.Printf("✅ Current balance: %.9f SOL (%d lamports)\n", balanceSOL, balance.Value)
	
	if balance.Value == 0 {
		fmt.Println("⚠️  ZERO BALANCE! Running in DEMO MODE...")
		fmt.Printf("💡 To run with real SOL:\n")
		fmt.Printf("   1. Visit: https://faucet.solana.com\n")
		fmt.Printf("   2. Enter address: %s\n", fromPubkey.String())
		fmt.Printf("   3. Click 'Request SOL'\n")
		fmt.Printf("   4. Wait for SOL to arrive and re-run this script\n")
		fmt.Println("\n🎭 DEMO MODE: Continuing with transaction simulation...")
	}
	
	transferLamports := uint64(TransferAmount * 1e9) // Convert SOL to lamports
	demoMode := false
	if balance.Value < transferLamports+5000 { // 5000 lamports for fees
		fmt.Printf("⚠️  Insufficient balance for transfer (need %.9f SOL + fees)\n", TransferAmount)
		fmt.Println("🎭 DEMO MODE: Will create and sign transaction but not broadcast")
		demoMode = true
	}
	
	// Step 4: Create Solana transaction
	fmt.Println("\n📍 Step 4: Creating Solana Transaction...")
	
	transfer := &SolanaTransfer{
		FromAddress: fromPubkey,
		ToAddress:   toPubkey,
		Amount:      transferLamports,
	}
	
	if err := createSolanaTransaction(ctx, client, transfer); err != nil {
		log.Fatal("Failed to create transaction:", err)
	}
	
	fmt.Printf("✅ Transaction created (transferring %.9f SOL)\n", TransferAmount)
	
	// Step 5: Generate MPC signature
	fmt.Println("\n📍 Step 5: Generating MPC Signature...")
	
	if err := signWithMPC(transfer); err != nil {
		log.Fatal("Failed to generate MPC signature:", err)
	}
	
	fmt.Printf("✅ MPC signature generated: %s\n", hex.EncodeToString(transfer.MPCSignature))
	
	// Step 6: Apply signature to transaction
	fmt.Println("\n📍 Step 6: Applying MPC Signature...")
	
	if err := applyMPCSignature(transfer); err != nil {
		log.Fatal("Failed to apply signature:", err)
	}
	
	fmt.Println("✅ Signature applied to transaction")
	
	// Step 7: Simulate transaction first
	fmt.Println("\n📍 Step 7: Simulating Transaction...")
	
	if err := simulateTransaction(ctx, client, transfer); err != nil {
		log.Fatal("Transaction simulation failed:", err)
	}
	
	fmt.Println("✅ Transaction simulation successful")
	
	// Step 8: Send transaction to devnet
	if demoMode {
		fmt.Println("\n📍 Step 8: Demo Mode - Showing Transaction Details...")
		
		// Serialize the full transaction for inspection
		txBytes, err := transfer.Transaction.MarshalBinary()
		if err != nil {
			log.Printf("Failed to serialize transaction: %v", err)
		} else {
			fmt.Printf("✅ Transaction ready for broadcast (%d bytes)\n", len(txBytes))
			fmt.Printf("✅ Transaction hash: %x\n", txBytes[:32])
		}
		
		fmt.Println("\n🎭 DEMO COMPLETE - Transaction Created & Signed with MPC!")
		fmt.Printf("===============================================\n")
		fmt.Printf("🔐 MPC Signature: %s\n", hex.EncodeToString(transfer.MPCSignature))
		fmt.Printf("💰 Transfer Amount: %.9f SOL\n", TransferAmount)
		fmt.Printf("📫 From: %s\n", transfer.FromAddress.String())
		fmt.Printf("📬 To: %s\n", transfer.ToAddress.String())
		fmt.Printf("\n💡 To broadcast this transaction:\n")
		fmt.Printf("   1. Get devnet SOL from https://faucet.solana.com\n")
		fmt.Printf("   2. Re-run this script with sufficient balance\n")
		
	} else {
		fmt.Println("\n📍 Step 8: Broadcasting to Solana Devnet...")
		
		sig, err := client.SendTransaction(ctx, transfer.Transaction)
		if err != nil {
			log.Fatal("Failed to send transaction:", err)
		}
		
		fmt.Printf("🎉 TRANSACTION SENT SUCCESSFULLY!\n")
		fmt.Printf("===============================\n")
		fmt.Printf("Transaction Signature: %s\n", sig.String())
		fmt.Printf("Solana Explorer: https://explorer.solana.com/tx/%s?cluster=devnet\n", sig.String())
		
		// Step 9: Wait for confirmation
		fmt.Println("\n📍 Step 9: Waiting for Confirmation...")
		
		if err := waitForConfirmation(ctx, client, sig); err != nil {
			log.Printf("⚠️  Error waiting for confirmation: %v", err)
		} else {
			fmt.Println("✅ Transaction confirmed on Solana devnet!")
		}
		
		// Step 10: Verify balance change
		fmt.Println("\n📍 Step 10: Verifying Transfer...")
		
		time.Sleep(2 * time.Second) // Wait a bit more for balance update
		newBalance, err := client.GetBalance(ctx, fromPubkey, rpc.CommitmentFinalized)
		if err != nil {
			log.Printf("Failed to get new balance: %v", err)
		} else {
			newBalanceSOL := float64(newBalance.Value) / 1e9
			fmt.Printf("✅ New balance: %.9f SOL (%d lamports)\n", newBalanceSOL, newBalance.Value)
			
			transferred := float64(balance.Value - newBalance.Value) / 1e9
			fmt.Printf("✅ Successfully transferred: %.9f SOL\n", transferred)
		}
	}
}

func createSolanaTransaction(ctx context.Context, client *rpc.Client, transfer *SolanaTransfer) error {
	// Get latest blockhash (replaces deprecated GetRecentBlockhash)
	latest, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get latest blockhash: %v", err)
	}
	
	// Create transfer instruction
	instruction := system.NewTransferInstruction(
		transfer.Amount,
		transfer.FromAddress,
		transfer.ToAddress,
	).Build()
	
	// Create transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		latest.Value.Blockhash,
		solana.TransactionPayer(transfer.FromAddress),
	)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %v", err)
	}
	
	transfer.Transaction = tx
	return nil
}

func signWithMPC(transfer *SolanaTransfer) error {
	// Get transaction message to sign
	messageToSign, err := transfer.Transaction.Message.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize transaction message: %v", err)
	}
	
	fmt.Printf("  📋 Message to sign: %s\n", hex.EncodeToString(messageToSign))
	
	// Setup EdDSA curve
	ed25519Curve, err := curve.NewEd25519()
	if err != nil {
		return fmt.Errorf("failed to create Ed25519 curve: %v", err)
	}
	defer ed25519Curve.Free()
	
	// Simulate MPC signing with S1 + S3
	nParties := 3
	partyNames := []string{"server", "pin-device", "offline-kms"}
	messengers := mocknet.NewMockNetwork(nParties)
	
	// Generate demo key shares for MPC
	type keygenResult struct {
		idx      int
		keyShare mpc.EDDSAMPCKey
		err      error
	}
	keygenCh := make(chan keygenResult, nParties)
	
	for i := 0; i < nParties; i++ {
		go func(partyIdx int) {
			job, err := mpc.NewJobMP(messengers[partyIdx], nParties, partyIdx, partyNames)
			if err != nil {
				keygenCh <- keygenResult{idx: partyIdx, err: err}
				return
			}
			defer job.Free()
			
			req := &mpc.EDDSAMPCKeyGenRequest{Curve: ed25519Curve}
			resp, err := mpc.EDDSAMPCKeyGen(job, req)
			if err != nil {
				keygenCh <- keygenResult{idx: partyIdx, err: err}
				return
			}
			
			keygenCh <- keygenResult{idx: partyIdx, keyShare: resp.KeyShare, err: nil}
		}(i)
	}
	
	// Collect key shares
	keyShares := make([]mpc.EDDSAMPCKey, nParties)
	for i := 0; i < nParties; i++ {
		result := <-keygenCh
		if result.err != nil {
			return fmt.Errorf("keygen failed for party %d: %v", result.idx, result.err)
		}
		keyShares[result.idx] = result.keyShare
	}
	
	// Perform MPC signing
	type signResult struct {
		idx       int
		signature []byte
		err       error
	}
	signCh := make(chan signResult, nParties)
	
	signatureReceiver := 0
	
	for i := 0; i < nParties; i++ {
		go func(partyIdx int) {
			job, err := mpc.NewJobMP(messengers[partyIdx], nParties, partyIdx, partyNames)
			if err != nil {
				signCh <- signResult{idx: partyIdx, err: err}
				return
			}
			defer job.Free()
			
			req := &mpc.EDDSAMPCSignRequest{
				KeyShare:          keyShares[partyIdx],
				Message:           messageToSign,
				SignatureReceiver: signatureReceiver,
			}
			
			resp, err := mpc.EDDSAMPCSign(job, req)
			if err != nil {
				signCh <- signResult{idx: partyIdx, err: err}
				return
			}
			
			signCh <- signResult{idx: partyIdx, signature: resp.Signature, err: nil}
		}(i)
	}
	
	// Collect signature
	for i := 0; i < nParties; i++ {
		result := <-signCh
		if result.err != nil {
			return fmt.Errorf("signing failed for party %d: %v", result.idx, result.err)
		}
		
		if result.idx == signatureReceiver && len(result.signature) > 0 {
			transfer.MPCSignature = result.signature
		}
	}
	
	if len(transfer.MPCSignature) != 64 {
		return fmt.Errorf("invalid signature length: got %d, expected 64", len(transfer.MPCSignature))
	}
	
	return nil
}

func applyMPCSignature(transfer *SolanaTransfer) error {
	// Convert MPC signature to Solana format
	signature := solana.Signature(transfer.MPCSignature)
	
	// Apply signature to transaction
	transfer.Transaction.Signatures = []solana.Signature{signature}
	
	return nil
}

func simulateTransaction(ctx context.Context, client *rpc.Client, transfer *SolanaTransfer) error {
	// Simulate the transaction
	result, err := client.SimulateTransaction(ctx, transfer.Transaction)
	if err != nil {
		return fmt.Errorf("simulation failed: %v", err)
	}
	
	if result.Value.Err != nil {
		return fmt.Errorf("simulation error: %v", result.Value.Err)
	}
	
	// Note: Fee estimation may not be available in all SDK versions
	fmt.Printf("  💰 Transaction simulation successful\n")
	
	return nil
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