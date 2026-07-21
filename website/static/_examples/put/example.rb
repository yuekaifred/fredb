uri = URI("https://db.fredyang.com/key/YOUR_KEY")
req = Net::HTTP::Put.new(uri)
req["X-Api-Key"] = "YOUR_API_KEY"
req.body = "YOUR_VALUE"
res = Net::HTTP.start(uri.host, uri.port) do |http|
  http.request(req)
end
