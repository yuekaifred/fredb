let resp = client.put("https://db.fredyang.com/key/YOUR_KEY")
    .header("X-Api-Key", "YOUR_API_KEY")
    .body("YOUR_VALUE")
    .send()?;
