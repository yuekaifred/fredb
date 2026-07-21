req, _ := http.NewRequest("PUT",
  "https://db.fredyang.com/key/YOUR_KEY",
  strings.NewReader("YOUR_VALUE"))
req.Header.Set("X-Api-Key", "YOUR_API_KEY")
resp, _ := http.DefaultClient.Do(req)
