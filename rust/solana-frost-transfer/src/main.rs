use anyhow::{anyhow, Result};
use clap::Parser;
use serde::Deserialize;
use solana_client::rpc_client::RpcClient;
use solana_sdk::{pubkey::Pubkey, system_instruction, transaction::Transaction};
use solana_sdk::signature::Signature as SolSignature;
use solana_sdk::message::Message;
use std::path::PathBuf;
use std::process::Command;
use hex::FromHex;
// we no longer reconstruct keys from shares inside this binary

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
struct Args {
    /// Comma-separated list of signer daemon base URLs
    #[arg(long)]
    signers: String,

    /// Path to group_public_key.json
    #[arg(long, default_value = "frost-artifacts/group_public_key.json")]
    group_key: PathBuf,

    #[arg(long)]
    to: Pubkey,

    #[arg(long, default_value_t = 1_000_000)]
    lamports: u64,

    #[arg(long, default_value = "https://api.devnet.solana.com")]
    rpc: String,

    /// Path to frost-aggregator binary (will be invoked to obtain signature)
    #[arg(long, default_value = "../frost-aggregator/target/release/frost-aggregator")]
    aggregator_bin: PathBuf,
}

#[derive(Debug, Deserialize)]
struct GroupKeyJson {
    verifying_key: String,
    address_base58: String,
}

// Transfer via external aggregator – no secret material ever loaded here.
fn main() -> Result<()> {
    let args = Args::parse();
    let client = RpcClient::new(args.rpc.clone());

    // Load group public key
    let key_json: GroupKeyJson = serde_json::from_reader(std::fs::File::open(&args.group_key)?)?;
    let pk_bytes: Vec<u8> = Vec::from_hex(&key_json.verifying_key)?;
    if pk_bytes.len() != 32 {
        return Err(anyhow!("verifying_key must be 32 bytes"));
    }
    let sender = Pubkey::new(pk_bytes.as_slice());
    let balance = client.get_balance(&sender)?;
    println!("Sender pubkey: {}", sender);
    println!(
        "Current balance: {} lamports ({} SOL)",
        balance,
        balance as f64 / 1_000_000_000f64
    );

    if balance < args.lamports {
        return Err(anyhow!(
            "Insufficient balance: trying to send {} lamports but only have {}",
            args.lamports,
            balance
        ));
    }

    let ix = system_instruction::transfer(&sender, &args.to, args.lamports);
    let recent_hash = client.get_latest_blockhash()?;

    let message = Message::new(&[ix], Some(&sender));
    let mut tx = Transaction::new_unsigned(message);
    tx.message.recent_blockhash = recent_hash;

    // Serialize message and ask aggregator to sign it
    let msg_bytes = tx.message.serialize();
    let msg_hex = hex::encode(&msg_bytes);

    let output = Command::new(&args.aggregator_bin)
        .arg("--msg-hex").arg(&msg_hex)
        .arg("--signers").arg(&args.signers)
        .arg("--group-key").arg(args.group_key.to_str().unwrap())
        .output()?;

    if !output.status.success() {
        return Err(anyhow!("aggregator process failed: {}", String::from_utf8_lossy(&output.stderr)));
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    // Expect line like "Aggregated signature (hex): <hex>"
    let sig_hex = stdout.trim().split_whitespace().last()
        .ok_or_else(|| anyhow!("unexpected aggregator output"))?;
    let sig_bytes_vec: Vec<u8> = Vec::from_hex(sig_hex)?;
    if sig_bytes_vec.len() != 64 {
        return Err(anyhow!("aggregated signature must be 64 bytes"));
    }
    let mut sig_bytes = [0u8; 64];
    sig_bytes.copy_from_slice(&sig_bytes_vec);
    let sol_sig = SolSignature::from(sig_bytes);
    tx.signatures = vec![sol_sig];

    let sig_sent = client.send_and_confirm_transaction(&tx)?;
    println!("Transaction submitted. Signature: {}", sig_sent);

    Ok(())
} 