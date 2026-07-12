#include "../engine/fredb.h"
#include <cstdio>
#include <cstring>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

static const char *DEFAULT_SOCK_PATH = "/tmp/fredb.sock";
static const char *DEFAULT_DATA_PATH = "/tmp/fredb-data";

static bool read_n_bytes(int fd, char *buf, size_t n) {
  size_t got = 0;
  while (got < n) {
    ssize_t r = recv(fd, buf + got, n - got, 0);
    if (r <= 0)
      return false;
    got += r;
  }
  return true;
}

static bool read_line(int fd, std::string &out) {
  out.clear();
  char c;
  while (true) {
    ssize_t r = recv(fd, &c, 1, 0);
    if (r <= 0)
      return false;
    if (c == '\n')
      return true;
    // handle dumb and stupid windows line endings
    if (c != '\r')
      out += c;
  }
}

static void send_n_bytes(int fd, const std::string &s) {
  size_t sent = 0;
  while (sent < s.size()) {
    ssize_t r = send(fd, s.c_str() + sent, s.size() - sent, 0);
    if (r <= 0)
      return;
    sent += r;
  }
}

static void handle(int fd, Fredb &db) {
  std::string line;
  while (read_line(fd, line)) {
    std::string reply;

    if (line.substr(0, 4) == "GET ") {
      std::string key = line.substr(4);
      auto val = db.get(key);
      if (val) {
        reply = std::to_string(val->size()) + "\n" + *val;
      } else {
        reply = "NOT_FOUND\n";
      }
    } else if (line.substr(0, 4) == "DEL ") {
      std::string key = line.substr(4);
      db.remove(key);
      reply = "OK\n";

    } else if (line.substr(0, 4) == "PUT ") {
      std::string rest = line.substr(4);
      auto sp = rest.find(' ');
      if (sp == std::string::npos) {
        reply = "ERR missing len\n";
      } else {
        std::string key = rest.substr(0, sp);
        size_t vlen = std::stoul(rest.substr(sp + 1));
        std::string val(vlen, '\0');
        if (!read_n_bytes(fd, val.data(), vlen))
          break;
        db.put(key, val);
        reply = "OK\n";
      }

    } else if (line.substr(0, 6) == "RANGE ") {
      std::string rest = line.substr(6);
      auto sp = rest.find(' ');
      if (sp == std::string::npos) {
        reply = "ERR missing end\n";
      } else {
        std::string start = rest.substr(0, sp);
        std::string end = rest.substr(sp + 1);
        auto pairs = db.get_range(start, end);
        reply = "COUNT " + std::to_string(pairs.size()) + "\n";
        for (auto &[k, v] : pairs)
          reply += k + "\n" + std::to_string(v.size()) + "\n" + v;
        reply += "END\n";
      }

    } else {
      reply = "ERR unknown command\n";
    }

    send_n_bytes(fd, reply);
  }
}

int main(int argc, char **argv) {
  const char *sock_path = DEFAULT_SOCK_PATH;
  const char *data_path = DEFAULT_DATA_PATH;

  for (int i = 1; i < argc; i++) {
    if (strcmp(argv[i], "-sock") == 0 && i + 1 < argc) {
      sock_path = argv[++i];
    } else if (strcmp(argv[i], "-data") == 0 && i + 1 < argc) {
      data_path = argv[++i];
    } else {
      fprintf(stderr, "usage: %s [-sock path] [-data path]\n", argv[0]);
      return 1;
    }
  }

  unlink(sock_path);

  int srv = socket(AF_UNIX, SOCK_STREAM, 0);
  if (srv < 0) {
    perror("socket");
    return 1;
  }

  sockaddr_un addr{};
  addr.sun_family = AF_UNIX;
  strncpy(addr.sun_path, sock_path, sizeof(addr.sun_path) - 1);

  if (bind(srv, (sockaddr *)&addr, sizeof(addr)) < 0) {
    perror("bind");
    return 1;
  }
  if (listen(srv, 8) < 0) {
    perror("listen");
    return 1;
  }

  Fredb db(data_path);

  while (true) {
    int fd = accept(srv, nullptr, nullptr);
    if (fd < 0)
      break;
    handle(fd, db);
    close(fd);
  }

  close(srv);
  unlink(sock_path);
}
