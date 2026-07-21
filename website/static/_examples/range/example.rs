let resp = client.get("https://db.fredyang.com/range")
    .query(&[("start", "START_KEY"), ("end", "END_KEY")])
    .header("X-Api-Key", "YOUR_API_KEY")
    .send()?;
