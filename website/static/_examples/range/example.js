fetch("https://db.fredyang.com/range?start=START_KEY&end=END_KEY", {
  headers: { "X-Api-Key": "YOUR_API_KEY" }
}).then(r => r.json())
