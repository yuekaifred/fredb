let resp = client.get("https://db.fredyang.com/key/YOUR_KEY")
    .header("X-Api-Key", "YOUR_API_KEY")
    .send()?;
