# GoCache

**GoCache** is a lightweight Redis clone implemented in Go. It provides a simplified yet functional key-value store with support for persistence, basic replication, transactions, and streams.

---

## 🚀 Features

1. **Key/Value Storage**:
   - `GET` and `SET` commands with support for setting expiry times.
2. **RDB Persistence**:
   - Load data from RDB files for persistent storage.
3. **Replication**:
   - Partial support for replication with basic master-replica setups.
4. **Streams**:
   - `TYPE`, `XADD`, `XREAD`, and `XRANGE` commands for basic stream operations.
5. **Transactions**:
   - Commands like `MULTI`, `EXEC`, `DISCARD`, and `INCR` for transactional workflows.

---

## 🛠 Prerequisites

- **Go**: Install the latest version from the [official website](https://go.dev/dl/).

---

## 🔧 Installation

Follow these steps to set up GoCache:

1. Clone the repository:
   ```bash
   git clone https://github.com/Pirate-Emperor/GoCache.git
   ```

2. Navigate to the project directory:
   ```bash
   cd GoCache
   ```

3. Build the project:
   ```bash
   go build -o goredis ./app
   ```

   The executable `goredis` will be created in the project directory.

---

## 🖥 Usage

Start the GoCache server:

```bash
./goredis
```

### ⚙️ Configuration Options

- **Port**:
  By default, the server binds to port `6379`. Use the `--port` flag to specify a different port:
  ```bash
  ./goredis --port 6380
  ```

- **RDB File**:
  Load an existing RDB file with the `--dir` (directory) and `--dbfilename` (filename) flags:
  ```bash
  ./goredis --dir /tmp/redis-files --dbfilename dump.rdb
  ```
  Default: `/tmp/redis-files/dump.rdb`.

- **Replication**:
  Start the server in replica mode with the `--replicaof` flag:
  ```bash
  ./goredis --replicaof "localhost 6379"
  ```

---

## 🛑 Limitations

- **Unsupported Commands**:
  - Hash operations like `HGET` and `HSET` are not implemented.
- **Replication**:
  - Partial replication support; commands like `WAIT` are not yet implemented.
- **RDB Saving**:
  - Server can load RDB files but does not support saving with the `SAVE` command.
- **Basic Feature Set**:
  - Limited to fundamental Redis-like features for simplicity.

---

## 📋 Supported Commands

### General

- `PING`: Test server connection.
- `ECHO`: Return the given string.

### Key/Value Operations

- `SET` and `GET`: Basic key-value operations.
- `INCR`: Increment integer values.

### Transactions

- `MULTI`: Start a transaction.
- `EXEC`: Execute queued commands in a transaction.
- `DISCARD`: Discard queued transaction commands.

### Streams

- `TYPE`: Identify the type of a key.
- `XADD`: Add a new entry to a stream.
- `XRANGE`: Retrieve entries within an ID range.
- `XREAD`: Read from streams (supports blocking and non-blocking).

### Configuration and Replication

- `CONFIG`: Get or set server configuration parameters.
- `INFO`: Display server statistics.
- `REPLCONF`: Configure replication settings.
- `PSYNC`: Partial synchronization for replicas.

---

## 🏗 Future Enhancements

- Implement advanced Redis commands, including `HGET`, `HSET`, and `WAIT`.
- Add support for RDB file saving.
- Expand replication support for comprehensive master-replica setups.

---

## 📚 Acknowledgments

- **Redis**: For inspiration and documentation.
- **HDT3213**: For the CRC64 checksum [implementation](https://github.com/HDT3213/rdb/blob/e5a00e130dda889ce1396d5561f95540418d12fc/crc64jones/crc64.go).

---

## 📜 License

This project is licensed under the **Pirate-Emperor License**. See the [LICENSE](LICENSE) file for details.

---

## 👤 Author


**Rahul Kumar Singh**  

[![Twitter](https://skillicons.dev/icons?i=twitter)](https://twitter.com/PirateKingRahul)
[![Discord](https://skillicons.dev/icons?i=discord)](https://discord.com/users/1200728704981143634)
[![LinkedIn](https://skillicons.dev/icons?i=linkedin)](https://www.linkedin.com/in/piratekingrahul)

[![Reddit](https://img.shields.io/badge/Reddit-FF5700?style=for-the-badge&logo=reddit&logoColor=white)](https://www.reddit.com/u/PirateKingRahul)
[![Medium](https://img.shields.io/badge/Medium-42404E?style=for-the-badge&logo=medium&logoColor=white)](https://medium.com/@piratekingrahul)

- 📧 Email: [piratekingrahul@gmail.com](mailto:piratekingrahul@gmail.com)  
- 📍 Location: Surat, Gujarat, India  
- 🌐 GitHub: [Rahul Kumar Singh](https://github.com/Pirate-Emperor)


---

Start using GoCache today and experience the power of lightweight key-value storage! 🌟