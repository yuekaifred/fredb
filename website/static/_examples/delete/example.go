req, _ := http.NewRequest("DELETE",
  "https://db.fredyang.com/key/YOUR_KEY", nil)
req.Header.Set("X-Api-Key", "YOUR_API_KEY")
resp, _ := http.DefaultClient.Do(req)
