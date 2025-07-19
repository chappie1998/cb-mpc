use std::{collections::BTreeMap, fs::File, path::PathBuf};

use anyhow::{anyhow, Result};
use clap::Parser;
use frost_ed25519 as frost;
use frost::keys::{PublicKeyPackage, VerifyingShare};
use frost::{round1::SigningCommitments, round2::SignatureShare};
use hex::FromHex;
use reqwest::Client;
use serde::{Deserialize, Serialize};
use tokio::runtime::Runtime;
use std::convert::TryInto;

/// JSON shapes identical to those served by signer daemons
#[derive(Serialize, Deserialize)]
struct NonceRequest {
    message: String,
}
#[derive(Serialize, Deserialize)]
struct NonceResponse {
    participant_id: String,
    commitments: SigningCommitments,
}

#[derive(Serialize, Deserialize)]
struct SignRequest {
    package: frost::SigningPackage,
}
#[derive(Serialize, Deserialize)]
struct SignResponse {
    share: SignatureShare,
}

/// group_public_key.json structure (minimal)
#[derive(Deserialize)]
struct GroupKeyFile {
    verifying_shares: std::collections::BTreeMap<String, String>,
    verifying_key: String,
}

#[derive(Parser, Debug)]
#[command(about = "Aggregator that collects FROST signature shares and outputs the aggregated signature.")]
struct Args {
    /// Hex-encoded message to sign (Schnorr pre-hash message)
    #[arg(long)]
    msg_hex: String,

    /// Comma-separated list of signer base URLs, e.g. http://127.0.0.1:3001
    #[arg(long)]
    signers: String,

    /// Path to group_public_key.json produced during DKG
    #[arg(long, default_value = "frost-artifacts/group_public_key.json")]
    group_key: PathBuf,
}

fn main() -> Result<()> {
    let args = Args::parse();

    let msg_bytes = <Vec<u8>>::from_hex(&args.msg_hex).map_err(|_| anyhow!("invalid msg_hex"))?;

    let rt = Runtime::new()?;
    let sig_bytes = rt.block_on(async { coordinator_run(&args, &msg_bytes).await })?;

    println!("Aggregated signature (hex): {}", hex::encode(sig_bytes));
    Ok(())
}

async fn coordinator_run(args: &Args, message: &[u8]) -> Result<Vec<u8>> {
    let client = Client::new();

    let signer_urls: Vec<String> = args
        .signers
        .split(',')
        .map(|s| s.trim().to_owned())
        .filter(|s| !s.is_empty())
        .collect();

    if signer_urls.len() < 2 {
        return Err(anyhow!("need at least two signer URLs"));
    }

    // 1. Round-1: ask each signer for its commitments
    let mut commitments_map: BTreeMap<frost::Identifier, SigningCommitments> = BTreeMap::new();
    for url in &signer_urls {
        let resp: NonceResponse = client
            .post(format!("{url}/nonce"))
            .json(&NonceRequest {
                message: hex::encode(message),
            })
            .send()
            .await?
            .json()
            .await?;

        let id_bytes = <Vec<u8>>::from_hex(resp.participant_id)?;
        if id_bytes.len() < 2 {
            return Err(anyhow!("identifier bytes too short"));
        }
        let id_u16 = u16::from_le_bytes([id_bytes[0], id_bytes[1]]);
        let identifier: frost::Identifier = id_u16.try_into().map_err(|e| anyhow!(format!("identifier err: {:?}", e)))?;

        commitments_map.insert(identifier, resp.commitments);
    }

    // Build SigningPackage
    let signing_package = frost::SigningPackage::new(commitments_map.clone(), message);

    // 2. Round-2: request signature share from each signer
    let mut shares: BTreeMap<frost::Identifier, SignatureShare> = BTreeMap::new();
    for url in &signer_urls {
        let id_resp: NonceResponse = client
            .post(format!("{url}/nonce"))
            .json(&NonceRequest {
                message: hex::encode(message),
            })
            .send()
            .await?
            .json()
            .await?;
        let id_bytes = <Vec<u8>>::from_hex(id_resp.participant_id)?;
        let id_u16 = u16::from_le_bytes([id_bytes[0], id_bytes[1]]);
        let identifier: frost::Identifier = id_u16.try_into().map_err(|e| anyhow!(format!("identifier err: {:?}", e)))?;

        // send sign request
        let sign_resp: SignResponse = client
            .post(format!("{url}/sign"))
            .json(&SignRequest {
                package: signing_package.clone(),
            })
            .send()
            .await?
            .json()
            .await?;

        shares.insert(identifier, sign_resp.share);
    }

    // 3. Load PublicKeyPackage from group_public_key.json
    let gkf: GroupKeyFile = serde_json::from_reader(File::open(&args.group_key)?)?;

    // Convert verifying_shares
    let mut verifying_shares: BTreeMap<frost::Identifier, VerifyingShare> = BTreeMap::new();
    for (id_hex, share_hex) in gkf.verifying_shares {
        let id_bytes = <Vec<u8>>::from_hex(&id_hex)?;
        if id_bytes.len() < 2 {
            return Err(anyhow!("identifier bytes too short in group key"));
        }
        let id_u16 = u16::from_le_bytes([id_bytes[0], id_bytes[1]]);
        let identifier: frost::Identifier = id_u16.try_into().map_err(|e| anyhow!(format!("identifier err: {:?}", e)))?;

        let share_bytes = <Vec<u8>>::from_hex(&share_hex)?;
        let verifying_share = VerifyingShare::deserialize(&share_bytes).map_err(|e| anyhow!(format!("verifying share deserialize err: {:?}", e)))?;
        verifying_shares.insert(identifier, verifying_share);
    }

    // Verifying key
    let verifying_key_bytes = <Vec<u8>>::from_hex(&gkf.verifying_key)?;
    let verifying_key = frost::VerifyingKey::deserialize(&verifying_key_bytes).map_err(|e| anyhow!(format!("verifying key deserialize err: {:?}", e)))?;

    let pubkey_package = PublicKeyPackage::new(verifying_shares, verifying_key);

    // 4. Aggregate
    let signature = frost::aggregate(&signing_package, &shares, &pubkey_package).map_err(|e| anyhow!(format!("aggregate err: {:?}", e)))?;

    Ok(signature.serialize().map_err(|e| anyhow!(format!("serialize err: {:?}", e)))?)
} 