      ******************************************************************
      * IMSPART2.CBL — IMS DL/I data-name SSA + IO-PCB fixture           *
      * (#5054 working-storage VALUE tracing, #5053 IO-PCB binding).     *
      * The function code and SSA are DATA ITEMS (not inline literals),  *
      * resolved through their WORKING-STORAGE VALUE clauses:            *
      *   WF-GU   VALUE 'GU  '  -> SELECT                                *
      *   WF-ISRT VALUE 'ISRT'  -> INSERT                                *
      *   WS-ROOT VALUE 'PARTROOT(PARTKEY =' -> segment PARTROOT         *
      * A CALL against IO-PCB binds a message-queue datastore (#5053).   *
      ******************************************************************
       IDENTIFICATION DIVISION.
       PROGRAM-ID. IMSPART2.

       ENVIRONMENT DIVISION.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01  WF-GU       PIC X(04) VALUE 'GU  '.
       01  WF-ISRT     PIC X(04) VALUE 'ISRT'.
       01  WS-ROOT     PIC X(20) VALUE 'PARTROOT(PARTKEY ='.
       01  WS-DETL     PIC X(09) VALUE 'PARTDETL'.
       01  PART-IO     PIC X(80).
       01  MSG-IO      PIC X(80).

       LINKAGE SECTION.
       01  IO-PCB.
           05  IO-PCB-LTERM      PIC X(08).
       01  DB-PCB.
           05  DB-PCB-DBD        PIC X(08).

       PROCEDURE DIVISION.
       GET-ROOT-BY-NAME.
           CALL 'CBLTDLI' USING WF-GU DB-PCB PART-IO WS-ROOT.

       INSERT-DETAIL-BY-NAME.
           CALL 'CBLTDLI' USING WF-ISRT DB-PCB PART-IO WS-DETL.

       READ-MESSAGE.
           CALL 'CBLTDLI' USING WF-GU IO-PCB MSG-IO.

       SEND-MESSAGE.
           CALL 'CBLTDLI' USING WF-ISRT IO-PCB MSG-IO.
