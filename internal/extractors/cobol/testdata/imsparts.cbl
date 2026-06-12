      ******************************************************************
      * IMSPARTS.CBL — IMS DB/DC (DL/I) segment I/O fixture (#4948).     *
      * Proves: EXEC DLI GU|GN|ISRT|REPL|DLET SEGMENT(<seg>) and         *
      * CALL 'CBLTDLI'/'AIBTDLI' USING <func> <pcb> <io> <ssa> emit      *
      * SCOPE.DataAccess segment entities (orm=ims-dli) with             *
      * ACCESSES_TABLE edges from the enclosing paragraph, plus db_read / *
      * db_write effects. A segment is the IMS analog of a DB2 table.     *
      ******************************************************************
       IDENTIFICATION DIVISION.
       PROGRAM-ID. IMSPARTS.

       ENVIRONMENT DIVISION.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01  WS-FUNC               PIC X(04).
       01  PART-IO-AREA          PIC X(80).

       LINKAGE SECTION.
       01  IO-PCB.
           05  IO-PCB-LTERM      PIC X(08).
       01  DB-PCB.
           05  DB-PCB-DBD        PIC X(08).

       PROCEDURE DIVISION.
       GET-ROOT.
           EXEC DLI GU SEGMENT(PARTROOT)
               WHERE (PARTKEY = WS-PARTKEY)
           END-EXEC.

       GET-NEXT-DETAIL.
           EXEC DLI GN SEGMENT(PARTDETL)
           END-EXEC.

       ADD-DETAIL.
           EXEC DLI ISRT SEGMENT(PARTDETL)
           END-EXEC.

       UPDATE-ROOT.
           EXEC DLI REPL SEGMENT(PARTROOT)
           END-EXEC.

       PURGE-DETAIL.
           EXEC DLI DLET SEGMENT(PARTDETL)
           END-EXEC.

       CALL-GET-UNIQUE.
           CALL 'CBLTDLI' USING 'GU  ' DB-PCB IO 'PARTROOT(KEY ='.

       CALL-INSERT.
           CALL 'AIBTDLI' USING 'ISRT' DB-PCB IO 'PARTDETL'.

       MSG-IN.
           CALL 'CBLTDLI' USING 'GU  ' IO-PCB PART-IO-AREA.
