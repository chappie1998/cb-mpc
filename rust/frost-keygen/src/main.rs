use frost_ed25519 as frost;
use frost::keys::KeyPackage;
use serde::Deserialize;
use anyhow::anyhow;
use std::fs::File;

#[derive(Deserialize)]
struct Stored {
    key_package: KeyPackage,
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 4 {
        eprintln!("Usage: frost-keygen <s1.json> <s2.json> <out-keypair.json>");
        std::process::exit(1);
    }

    // Load the two share files
    let s1: Stored = serde_json::from_reader(File::open(&args[1])?)?;
    let s2: Stored = serde_json::from_reader(File::open(&args[2])?)?;

    // Reconstruct the signing key (scalar)
    let signing_key = frost::keys::reconstruct(&[s1.key_package, s2.key_package])
        .map_err(|e| anyhow!(format!("reconstruct error: {e:?}")))?;

    // Serialize scalar -> 32 bytes
    let bytes = signing_key.serialize();
    let sk_bytes: [u8; 32] = bytes.as_slice().try_into().expect("scalar len 32");

    // Compute group public key as defined by FROST (same result as group_public_key.json)
    let verifying_key = frost::VerifyingKey::from(&signing_key);
    let pk_bytes = verifying_key
        .serialize()
        .map_err(|e| anyhow!(format!("pk serialize error: {e:?}")))?; // 32 bytes

    // Solana keypair format: [secret||public]
    let mut out_vec = Vec::with_capacity(64);
    out_vec.extend_from_slice(&sk_bytes);
    out_vec.extend_from_slice(&pk_bytes);

    serde_json::to_writer_pretty(File::create(&args[3])?, &out_vec)?;
    println!("✅ Keypair written to {}", &args[3]);
    Ok(())
}
