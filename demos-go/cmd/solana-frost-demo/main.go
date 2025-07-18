package main

import (
    "bufio"
    "bytes"
    "context"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "log"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/gagliardetto/solana-go"
    "github.com/gagliardetto/solana-go/programs/system"
    "github.com/gagliardetto/solana-go/rpc"
)

// PublicKeyPackage mirrors frost_ed25519::keys::PublicKeyPackage (only the group public key bytes we need)
// The JSON structure produced by the Rust CLI is {"group_public_key":"<base64>" , ... }
// For simplicity we unmarshal into this struct.
type rustPubKeyPackage struct {
    GroupPublicKey []byte `json:"group_public_key"`
}

func main() {
    if len(os.Args) < 5 {
        fmt.Printf("Usage: %s <share1.json> <share3.json> <group_public_key.json> <recipient-base58>\n", os.Args[0])
        os.Exit(1)
    }
    share1 := os.Args[1]
    share3 := os.Args[2]
    pubkeyFile := os.Args[3]
    recipient := solana.MustPublicKeyFromBase58(os.Args[4])

    // ---------- Load group public key ----------
    var pkPkg rustPubKeyPackage
    data, err := os.ReadFile(pubkeyFile)
    if err != nil {
        log.Fatalf("failed to read public key json: %v", err)
    }
    if err := json.Unmarshal(data, &pkPkg); err != nil {
        log.Fatalf("failed to parse public key json: %v", err)
    }
    if len(pkPkg.GroupPublicKey) != 32 {
        log.Fatalf("unexpected group public key length: %d", len(pkPkg.GroupPublicKey))
    }
    mpcPubKey := solana.PublicKeyFromBytes(pkPkg.GroupPublicKey)
    fmt.Printf("🔐 MPC wallet address: %s\n", mpcPubKey.String())

    client := rpc.New(rpc.DevNet_RPC)

    // ---------- Build transaction ----------
    // Fetch latest blockhash
    bhResp, err := client.GetLatestBlockhash(context.Background())
    if err != nil {
        log.Fatalf("failed to get blockhash: %v", err)
    }

    amount := uint64(100_0000) // 0.001 SOL
    tx, err := solana.NewTransaction([]solana.Instruction{
        system.NewTransferInstruction(amount, mpcPubKey, recipient).Build(),
    }, bhResp.Value.Blockhash, solana.TransactionPayer(mpcPubKey))
    if err != nil {
        log.Fatalf("failed to build tx: %v", err)
    }

    // Serialize message bytes for signing
    msgBytes, err := tx.Message.MarshalBinary()
    if err != nil {
        log.Fatalf("failed to marshal msg bytes: %v", err)
    }

    // ---------- Call Rust FROST signer ----------
    sigBytes, err := frostSign(share1, share3, msgBytes)
    if err != nil {
        log.Fatalf("signing failed: %v", err)
    }
    if len(sigBytes) != 64 {
        log.Fatalf("invalid signature length: %d", len(sigBytes))
    }

    var sig solana.Signature
    copy(sig[:], sigBytes)
    tx.Signatures = []solana.Signature{sig}

    // ---------- Broadcast ----------
    sigHash, err := client.SendTransaction(context.Background(), tx)
    if err != nil {
        log.Fatalf("failed to send tx: %v", err)
    }
    fmt.Printf("📡 submitted tx: %s\n", sigHash.String())
    fmt.Printf("🔗 https://explorer.solana.com/tx/%s?cluster=devnet\n", sigHash.String())
}

func frostSign(share1Path, share3Path string, message []byte) ([]byte, error) {
    cliBin := filepath.Join("rust", "frost-ed25519-cli", "target", "release", "frost-ed25519-cli")
    // Ensure built binary exists; if not, attempt cargo build.
    if _, err := os.Stat(cliBin); os.IsNotExist(err) {
        fmt.Println("ℹ️ building Rust signer...")
        cmdB := exec.Command("cargo", "build", "--release")
        cmdB.Dir = filepath.Join("rust", "frost-ed25519-cli")
        cmdB.Stdout = os.Stdout
        cmdB.Stderr = os.Stderr
        if err := cmdB.Run(); err != nil {
            return nil, fmt.Errorf("failed to build rust signer: %w", err)
        }
    }

    hexMsg := hex.EncodeToString(message)
    cmd := exec.Command(cliBin, "sign", share1Path, share3Path, hexMsg)
    var out bytes.Buffer
    cmd.Stdout = &out
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        return nil, err
    }
    sigHex := bufio.NewScanner(&out)
    sigHex.Scan()
    sigStr := sigHex.Text()
    sigBytes, err := hex.DecodeString(sigStr)
    if err != nil {
        return nil, fmt.Errorf("invalid signature hex from rust: %w", err)
    }
    return sigBytes, nil
} 