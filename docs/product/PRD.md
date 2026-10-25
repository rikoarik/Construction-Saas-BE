# Product Requirements Document (PRD) — Construction SaaS

**ID:** PRD-001 · **Versi:** 1.0.0 · **Tanggal:** 2026-10-24 · **Status:** Proposed / belum diimplementasikan  
**Owner keputusan:** Product Owner · **Sasaran:** SaaS B2B multi-tenant untuk semua jenis kontraktor di Indonesia  
**Rujukan:** [Blueprint](BLUEPRINT.md) · [System Design](../architecture/SYSTEM-DESIGN.md) · [Tasks](../delivery/TASKS.md)

## 1. Masalah, peluang, dan tujuan

Pekerjaan proyek sering direkap dari berbagai spreadsheet: RAB/BoQ, time schedule, laporan harian, kemajuan mingguan, dan kurva-S. Penyalinan ulang menyebabkan perbedaan angka, laporan lambat, approval tak terdokumentasi dan foto lapangan terpisah dari capaian volume. Dokumen contoh dari percakapan berisi **Laporan Mingguan Kemajuan Fisik dan Biaya** serta **Time Schedule/Kurva-S**; keduanya menjadi keluaran resmi inti produk. Asumsi kerja: proses contoh berbasis Excel dan manual; perlu divalidasi selama pilot.

**Tujuan produk:** jadikan anggaran disahkan, rencana, progres terverifikasi, dan artefak ekspor sebagai satu alur terhubung yang terlacak. Nilai *biaya pekerjaan* dalam laporan kemajuan berarti nilai pekerjaan terukur dari harga satuan kontrak, **bukan** cash outflow / biaya pengeluaran aktual.

### Outcome dan kriteria pilot (bukan klaim performa saat ini)
1. Pilot minimal **dua karakter proyek berbeda** (misalnya gedung dan jalan) memakai model data inti sama, tanpa fork kode per jenis proyek.
2. Data dari 2 format Excel RAB berbeda dapat diimpor via mapping, dengan validasi dan preview sebelum menjadi draft.
3. Satu laporan terverifikasi hanya mem-posting ledger **sekali**, termasuk ketika request/webhook diulang.
4. Backend menghasilkan angka yang sama pada dashboard, XLSX dan PDF dari snapshot identik; cocok dengan sampel Excel yang diverifikasi manusia.
5. Negative tests membuktikan pengguna tenant A tak dapat mengakses record, ekspor, atau lampiran tenant B.
6. Pengguna lapangan dapat melakukan input → submit via HP/Telegram Mini App; supervisor menyetujui via web atau UI yang tersedia.

**Scope philosophy:** product requirements cover the complete SaaS. Capabilities may ship in dependency-based stages; a later-stage capability must still fit the same domain and security boundaries. Features requiring separate legal/provider decisions (for example certified e-signature or additional messaging providers) remain explicit decisions rather than hidden assumptions.

## 2. Audience, konteks, dan jobs-to-be-done

| Persona | JTBD | Kanal utama | Pain point |
|---|---|---|---|
| Company Owner/Admin | Beri akses tim, awasi proyek perusahaan | Web | Data tersebar, izin sulit dikendalikan |
| Project Manager | Bandingkan rencana/realisasi dan selesaikan hambatan | Web | Angka tidak up-to-date/berbeda antarfile |
| QS/Estimator | Impor dan revisi RAB, bobot, jadwal | Web | Format Excel vendor bervariasi |
| Supervisor/Pengawas | Periksa volume dan bukti sebelum disahkan | Web/mobile | Riwayat revisi & bukti tidak rapi |
| Mandor/Reporter | Laporkan pekerjaan secara cepat di lapangan | Telegram Mini App | Sulit mengedit spreadsheet via HP |
| Viewer/Stakeholder | Lihat laporan yang memang diberikan akses | Web | Membutuhkan laporan formal dan jelas |
| Platform Operator | Melihat kesehatan layanan, bukan isi data pelanggan | Console operasional | Kebocoran data tenant harus dicegah |

Lihat [Personas & User Stories](PERSONAS-AND-STORIES.md) untuk cerita pengguna, alur dan permission per aksi.

## 3. Problem scenarios

- Kontraktor gedung mengatur pekerjaan per lantai/zona dengan satuan m², m³, kg, titik.
- Kontraktor jalan mengatur STA/segmen dengan satuan m, m³, ton.
- Perusahaan memiliki kop surat dan penanggung jawab dokumen yang berbeda, tetapi item pekerjaan dan nilai tetap memakai perhitungan standar.
- Mandor melaporkan 19,04 m³ pekerjaan beton pada minggu ini, sebelumnya 63,00 m³; jika kontrak 82,04 m³, item selesai 100% setelah approval (bukan saat submit).
- Kontrak mengalami addendum; laporan yang telah terbit wajib tetap dapat direproduksi sesuai versi anggaran/baseline lama, dan penerbitan ulang eksplisit.

## 4. Functional requirements

| ID | Product requirement | Acceptance summary | Primary lanes |
|---|---|---|---|
| FR-001 | Multi-tenant company, user, invitation, company/project roles | Default-deny authorization; multi-company supported | BE/FE/INT |
| FR-002 | Secure Go authentication/session lifecycle | Argon2id, opaque revocable sessions, CSRF/rate controls | BE/FE |
| FR-003 | Project, contract, team, location and metadata management | All mutations scoped to authorized company/project | BE/FE |
| FR-004 | Flexible templates, hierarchical WBS, zones/STA/locations | Building/road/infrastructure work without code fork | BE/FE |
| FR-005 | XLSX RAB/BoQ import, mapping, preview and validation | Multiple spreadsheet layouts normalize safely | BE/FE/INT |
| FR-006 | Immutable approved budget versions and addenda | Historic versions remain reproducible | BE/FE |
| FR-007 | Weekly planning, baseline approval and rebaseline history | Calendar/timezone correct and versioned | BE/FE |
| FR-008 | Field reporting with typed measurements, evidence and custom fields | Draft/submit/revision lifecycle works on mobile | BE/FE |
| FR-009 | Versioned approval workflow and audit history | Unauthorized/self/cross-tenant approval denied | BE/FE |
| FR-010 | Exactly-once official progress posting | Retry/concurrency cannot duplicate ledger effect | BE/INT |
| FR-011 | Weighted progress, previous/current/cumulative, deviation and S-curve | Dashboard derives from approved ledger/baseline | BE/FE/INT |
| FR-012 | Weekly physical/value XLSX/PDF | Same immutable snapshot drives all outputs | BE/FE |
| FR-013 | Time Schedule/S-curve XLSX/PDF | Plan/actual/deviation series match dashboard snapshot | BE/FE |
| FR-014 | Telegram account link, Bot, Mini App and notifications | Signed/fresh input, replay-safe webhook, scoped reporting | BE/FE/INT |
| FR-015 | Audit, private attachments, secure generated documents and support access | No unauthorized media/export/support access | BE/FE/INT |
| FR-016 | Actual costs, commitments, vendors and cost-vs-progress views | Actual cost is separate from earned-value/progress | BE/FE/INT |
| FR-017 | Material catalog, receipt/issue/return/transfer/adjustment ledger | Stock balances reconcile from append-only movements | BE/FE/INT |
| FR-018 | Versioned report branding, signatories and advanced template configuration | Published history unaffected by later template changes | BE/FE/INT |
| FR-019 | SaaS plans, subscriptions, usage and entitlements | Backend enforces limits; UI reflects canonical capability state | BE/FE/INT |
| FR-020 | Enterprise/scale capabilities: SSO, delegated approval, audit export, retention, quotas | Extensions preserve tenant/audit/security boundaries | BE/FE/INT |
| FR-021 | English/Indonesian localized UI and light/dark/system themes | No hardcoded UI copy; critical flows pass theme/locale review | FE/INT |

Product types/templates are configuration presets, not code forks. The complete product is delivered in dependency-based stages; later capabilities use the same domain, security, localization, audit, and versioning contracts rather than a separate architecture.

## 5. Non-functional requirements

| ID | Area | Requirement / cara verifikasi |
|---|---|---|
| NFR-001 | Isolation | Authz company+project di Go pada semua endpoint; composite FK; negative integration tests untuk resource, file, job dan export |
| NFR-002 | Integrity | Uang/qty/bobot pakai PostgreSQL NUMERIC + decimal-safe Go, tanpa float64 authoritative |
| NFR-003 | Audit | Persetujuan, koreksi, role dan publikasi dokumen dicatat append-only |
| NFR-004 | Idempotency | Approval, webhook, ledger posting, generation job tahan retry |
| NFR-005 | Availability | Health/readiness, restartable worker, backup dan restore drill sebelum production berbayar |
| NFR-006 | Accessibility | Web mobile-friendly, label form, keyboard/navigation, kontras dan feedback validasi |
| NFR-007 | Security | Password hash Argon2id, Secure/HttpOnly cookie, CSRF defense, upload validation, secret management, Telegram signed initData |
| NFR-008 | Consistency | Dokumen resmi dihitung dari immutable snapshot; dashboard boleh eventual consistency dengan `updated_at` |
| NFR-009 | Performance | Tetapkan target p95 API/export dari dataset pilot, beban realistis dan load test; **target numerik TBD**, jangan klaim SLA |
| NFR-010 | Compliance | Pajak configurable, kebijakan retensi dan legal signoff menunggu review; jangan hardcode tarif |

## 6. Primary user journeys

1. **Onboarding:** registrasi owner → company → undang anggota → set role → login → akses hanya proyek terkait.
2. **Project setup:** buat proyek → pilih template → definisikan WBS/lokasi → impor XLSX RAB → mapping & preview → review/approve budget → buat & approve baseline.
3. **Progress:** mandor buka Mini App → validasi identitas → isi form & foto → submit → notifikasi supervisor → periksa/return/reject/approve → posting ledger → tampilkan dashboard.
4. **Weekly reporting:** pilih periode & snapshot versi → render tabel biaya pekerjaan + time schedule → cek preview → review/publish → file privat dan jejak audit.
5. **Amendment:** draft budget baru + alasan → review & approve → konfigurasi cutover/effective date → laporan baru mengacu versi berlaku; laporan lama tetap tidak berubah.

Lebih rinci: [STATE-MACHINES](../specs/STATE-MACHINES.md) dan [SYSTEM-DESIGN](../architecture/SYSTEM-DESIGN.md).

## 7. Business rules dan pengukuran

- Bobot = nilai leaf item / total approved RAB eligible ×100; header/subtotal tidak dihitung dua kali.
- Item quantity = cumulative approved qty / contracted qty ×100. Item method `milestone` dan `verified_percent` butuh bukti serta approval; penggabungan metode harus didefinisikan dengan bobot sama.
- Project progress = Σ kontribusi item (`weight × item_pct/100`).
- Weekly previous / current / cumulative dihitung dari `progress_ledger` berdasar date range/cutoff dan timezone proyek.
- Deviasi (pp) = kumulatif realisasi (%) − kumulatif rencana (%). Jangan menyebut pp sebagai persen pertumbuhan relatif.
- Qty melebihi kontrak ditandai, tidak diam-diam dipangkas; zero denominator ditolak/ditangani eksplisit.
- Angka snapshot dan aturan pembulatan perlu disepakati lewat golden fixtures. Lihat [CALCULATION-RULES](../architecture/CALCULATION-RULES.md).

## 8. Release gates

- Semua task yang termasuk pada release target berstatus `verified` dengan bukti PR/test, bukan sekadar dokumen/mockup.
- Tidak ada isu severity kritis pada tenant isolation, auth, approval, export.
- Sampel Excel menjadi golden data yang disetujui dan versi template laporan ditetapkan.
- Uji lintas dua jenis proyek; cross-tenant negative test; login+Mini App; backup restore drill; audit & rollback.
- Sign-off Product Owner dan technical reviewer. Lihat [ACCEPTANCE-CRITERIA](../delivery/ACCEPTANCE-CRITERIA.md).

## 9. Product capability stages

The complete scope is implemented in dependency-driven stages: engineering foundation; project controls; field operations; progress and official reporting; cost/material control; SaaS commercial administration; enterprise/scale hardening. Stage order does not redefine the product model. See [Implementation Plan](../delivery/IMPLEMENTATION-PLAN.md).

## 10. Open decisions

1. Aturan penilaian addendum lintas periode / harga baru terhadap progres historis.
2. Batas rounding & residual assignment pada bobot dan ekspor per kategori.
3. Dukungan pajak/nilai kontrak per jenis kontrak; perlu review ahli administrasi kontrak.
4. Kebijakan koreksi laporan resmi, legal pengesahan, retensi data dan lokasi penyimpanan.
5. Target SLO/p95/perusahaan & strategi deployment berdasarkan kebutuhan pilot.

Dicatat di [RISKS-AND-DECISIONS](../delivery/RISKS-AND-OPEN-DECISIONS.md), tidak diselesaikan dengan asumsi tersembunyi.


## 11. Delivery ownership

Backend work is tracked in `docs/delivery/BACKEND-TASKS.md`, frontend in `docs/delivery/FRONTEND-TASKS.md`, and shared contracts/integration in `docs/delivery/INTEGRATION-TASKS.md`. Frontend and backend may execute concurrently after the relevant API contract is marked `contract-ready`.
