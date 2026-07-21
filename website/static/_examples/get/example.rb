uri = URI("https://db.fredyang.com/key/YOUR_KEY")
req = Net::HTTP::Get.new(uri)
req["X-Api-Key"] = "YOUR_API_KEY"
res = Net::HTTP.start(uri.host, uri.port) do |http|
  http.request(req)
end
