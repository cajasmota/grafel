import pymysql

conn = pymysql.connect(host="localhost", user="root", db="shop")
cursor = conn.cursor()

cursor.execute("""
    CREATE TABLE IF NOT EXISTS users (
        id INT AUTO_INCREMENT PRIMARY KEY,
        username VARCHAR(150) NOT NULL,
        email VARCHAR(255) NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE KEY uq_email (email)
    )
""")

cursor.execute("""
    CREATE TABLE orders (
        id INT PRIMARY KEY,
        user_id INT NOT NULL,
        total DECIMAL(10, 2)
    )
""")
