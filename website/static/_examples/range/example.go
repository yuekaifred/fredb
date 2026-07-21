req, _ := http.NewRequest("GET",
  "https://db.fredyang.com/range?start=START_KEY&end=END_KEY", nil)
req.Header.Set("X-Api-Key", "YOUR_API_KEY")
resp, _ := http.DefaultClient.Do(req)
