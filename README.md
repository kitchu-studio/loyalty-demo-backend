# loyalty-demo-backend

API สำหรับระบบ **สะสมแต้ม + ผู้ช่วย AI ตอบลูกค้า** ของร้านเล็ก (ร้านกาแฟ ร้านอาหาร ร้านเสริมสวย) เขียนด้วย Go ใช้คู่กับแอป React Native ใน [loyalty-demo-app](https://github.com/kitchu-studio/loyalty-demo-app)

เป็นโปรเจกต์ตัวอย่างสำหรับโชว์งาน ไม่ใช่ระบบที่เปิดใช้จริงของร้านใดร้านหนึ่ง

## ทำอะไรได้

- **สะสมแต้ม** หลังลูกค้าจ่ายเงิน พนักงานพิมพ์ยอดบิลแล้วได้ **โค้ด 6 ตัว** ให้ลูกค้ากรอกในแอป โค้ดใช้ได้ครั้งเดียวและหมดอายุใน 24 ชั่วโมง (ทุก N บาท = 1 แต้ม ตั้งค่าได้)
- **แลกของรางวัล** หักแต้ม ได้ **คูปอง** ที่ลูกค้าโชว์หน้าร้าน พนักงานกดยืนยันว่ารับของแล้ว คูปองใช้ซ้ำไม่ได้
- **ผู้ช่วย AI** ลูกค้าถามเรื่องเวลาเปิด-ปิด เมนู โปรโมชัน หรือแต้มของตัวเอง ระบบตอบจากข้อมูลร้านที่เจ้าของแก้ได้เองจากแอป ถ้าไม่มี `ANTHROPIC_API_KEY` จะตอบแบบตัวอย่างจากกฎง่าย ๆ จึงรันและลองใช้ได้ฟรี
- **หน้าร้าน** สรุปจำนวนสมาชิก แต้มที่แจก และคูปองที่ค้างอยู่

## ตัดสินใจเชิงออกแบบที่ควรรู้

| เรื่อง | ทำอย่างไร | เพราะอะไร |
|---|---|---|
| ยอดแต้ม | เก็บเป็น **ledger** (ทุกการเพิ่ม/ลดคือหนึ่งแถว) ยอดคงเหลือคือผลรวม | ย้อนดูได้เสมอว่าแต้มมาจากไหน ไม่มียอดไม่ตรงกับประวัติ |
| แลกแต้มพร้อมกัน | ล็อกแถวผู้ใช้ใน transaction ก่อนตรวจยอด | กดแลกสองครั้งติดกันก็ใช้แต้มซ้ำไม่ได้ |
| โค้ดสะสมแต้ม | สุ่มจากตัวอักษรที่ไม่สับสน (ไม่มี I L O 0 1) และจำกัดการเดา 10 ครั้ง/นาที | อ่านจากใบเสร็จง่าย และเดาสุ่มยาก |
| ผู้ช่วย AI | เซิร์ฟเวอร์ประกอบ system prompt เอง (ข้อมูลร้าน แต้มของคนที่ถาม) จำกัด 12 ข้อความ/นาที/คน | แอปแก้ข้อมูลให้ AI เห็นไม่ได้ และกันค่าใช้จ่ายบานปลาย |
| AI ล่ม | ตอบด้วยโหมดตัวอย่างแทนที่จะขึ้น error | หน้าแชตไม่พังเพราะผู้ให้บริการมีปัญหาชั่วคราว |

## เทคโนโลยี

Go 1.25 · Gin · PostgreSQL 16 (pgx) · JWT (access 15 นาที / refresh 30 วัน) · golang-migrate · Docker Compose · Caddy (HTTPS อัตโนมัติ) · Claude API (ไม่บังคับ)

## รันในเครื่อง

ต้องมี Docker

```bash
cp .env.example .env        # แก้ JWT_SECRET และ ADMIN_EMAILS
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
curl http://localhost:8081/healthz
```

API จะอยู่ที่ `http://localhost:8081` (แอปบน Android emulator เรียกผ่าน `http://10.0.2.2:8081`) migration และข้อมูลตัวอย่าง (ของรางวัล 4 อย่าง ข้อมูลร้าน) ถูกสร้างให้เอง

บัญชีที่สมัครด้วยอีเมลใน `ADMIN_EMAILS` จะเป็นพนักงานร้าน

### เปิดใช้ผู้ช่วย AI จริง

ใส่ `ANTHROPIC_API_KEY=...` ใน `.env` แล้วรัน compose ใหม่ (เปลี่ยนโมเดลได้ที่ `ANTHROPIC_MODEL` ค่าเริ่มต้น `claude-haiku-5-5`)

## API

ทุกเส้นทางอยู่ใต้ `/api/v1` เส้นที่มี 🔒 ต้องส่ง `Authorization: Bearer <token>`

| Method | Path | ใครใช้ | หน้าที่ |
|---|---|---|---|
| POST | `/auth/register` `/auth/login` `/auth/refresh` | ทุกคน | สมัคร เข้าสู่ระบบ ต่ออายุ token |
| GET | `/shop` | ทุกคน | ชื่อร้าน ข้อมูลร้าน อัตราแต้ม |
| GET | `/rewards` | ทุกคน | ของรางวัลที่แลกได้ |
| GET | `/me` 🔒 | สมาชิก | โปรไฟล์ + แต้มคงเหลือ |
| GET | `/points/history` 🔒 | สมาชิก | ประวัติแต้ม 50 รายการล่าสุด |
| POST | `/points/claim` 🔒 | สมาชิก | กรอกโค้ดจากพนักงานเพื่อรับแต้ม |
| POST | `/rewards/:id/redeem` 🔒 | สมาชิก | แลกของรางวัล ได้คูปอง |
| GET | `/redemptions` 🔒 | สมาชิก | คูปองของฉัน |
| POST | `/chat` 🔒 | สมาชิก | ถามผู้ช่วย AI |
| POST | `/admin/earn-codes` 🔒 | พนักงาน | ออกโค้ดสะสมแต้มจากยอดบิล |
| GET | `/admin/redemptions/:code` 🔒 | พนักงาน | ดูคูปองว่าของใคร ใช้แล้วหรือยัง |
| POST | `/admin/redemptions/:code/use` 🔒 | พนักงาน | ยืนยันว่าส่งมอบของแล้ว |
| GET | `/admin/summary` 🔒 | พนักงาน | สรุปสมาชิก แต้ม คูปอง |
| PUT | `/admin/shop-about` 🔒 | พนักงาน | แก้ข้อมูลร้านที่ผู้ช่วย AI รู้ |

ตัวอย่าง

```bash
# พนักงานออกโค้ดจากบิล 250 บาท -> ลูกค้าได้ 25 แต้ม
curl -X POST http://localhost:8081/api/v1/admin/earn-codes \
  -H "Authorization: Bearer $STAFF_TOKEN" -H "Content-Type: application/json" \
  -d '{"amount_baht": 250}'

# ลูกค้ากรอกโค้ด
curl -X POST http://localhost:8081/api/v1/points/claim \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"code": "K7P2QM"}'
```

## เทสต์

```bash
go test ./...
```

กฎคำนวณแต้ม การสุ่มโค้ด การเรียก Claude API (ด้วยเซิร์ฟเวอร์จำลอง) และการเตรียมบทสนทนา มีเทสต์ครอบคลุมในแพ็กเกจ `internal/loyalty` และ `internal/ai`

## ขึ้นเซิร์ฟเวอร์จริง

`docker compose up -d --build` บนเซิร์ฟเวอร์ (EC2 / VPS) โดยตั้ง `DOMAIN` เป็นโดเมนจริงที่ชี้มาที่เครื่อง Caddy จะขอใบรับรอง HTTPS ให้เอง อย่าใช้ `docker-compose.dev.yml` บนเซิร์ฟเวอร์จริง

## โครงสร้าง

```
cmd/api            จุดเริ่มโปรแกรม
internal/config    อ่านค่าจาก environment
internal/auth      JWT และ bcrypt
internal/loyalty   กฎแต้ม การสุ่มโค้ด (ไม่ผูกกับ HTTP/DB)
internal/ai        เรียก Claude API, system prompt, โหมดตัวอย่าง
internal/store     คำสั่ง SQL ทั้งหมด
internal/httpapi   routes, handlers, rate limit
migrations         SQL migration
```
